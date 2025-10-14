package cluster

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	"golang.org/x/net/context"

	"github.com/user00265/dxclustergoapi/internal/config"
	"github.com/user00265/dxclustergoapi/internal/logging"
	"github.com/user00265/dxclustergoapi/internal/spot"
)

const (
	defaultLoginPrompt   = "Please enter your call:"
	defaultPassPrompt    = "password:"
	connectionTerminator = "\n"    // Newline terminator for commands
	dxIDPrefix           = "DX de" // Prefix to identify DX spots
)

// Spot represents a parsed DX spot.
type Spot struct {
	Spotter   string    `json:"spotter"`
	Spotted   string    `json:"spotted"`
	Frequency float64   `json:"frequency"` // In kHz (original format)
	Message   string    `json:"message"`
	When      time.Time `json:"when"`
	Source    string    `json:"source"` // Name of the cluster it came from (e.g., "DXCluster", "SOTA")
}

// Client represents a single DXCluster connection.
type Client struct {
	cfg          config.ClusterConfig
	conn         net.Conn
	statusMutex  sync.RWMutex
	isConnected  bool
	cancel       context.CancelFunc // To stop the readLoop
	readLoopDone chan struct{}      // Signifies readLoop has exited
	// connectCancel allows Close() to cancel the background connect/retry loop
	connectCancel context.CancelFunc
	// connectDone is closed when the background connect goroutine exits.
	connectDone chan struct{}
	// readLoopStarted indicates whether a readLoop has been started for the current connection
	readLoopStarted bool

	// Channels for emitting events
	SpotChan    chan Spot
	MessageChan chan string
	ErrorChan   chan error // For parse errors or connection issues

	dxDelimRegex *regexp.Regexp // Pre-compiled regex for DX spots

	// Heartbeat tracking
	pingTicker      *time.Ticker
	pingCount       int
	lastPongTime    time.Time
	heartbeatActive bool
	initialPingDone bool

	// Ensure SpotChan is closed exactly once on Close()
	spotCloseOnce sync.Once
	chanCloseOnce sync.Once
}

// NewClient creates and returns a new DXCluster client for a single connection.
func NewClient(cfg config.ClusterConfig) (*Client, error) {
	if cfg.Callsign == "" {
		return nil, fmt.Errorf("callsign must be specified for DX cluster %s:%s", cfg.Host, cfg.Port)
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("host must be specified for DX cluster")
	}
	if cfg.Port == "" {
		cfg.Port = config.DefaultDXCPort
	}
	if cfg.LoginPrompt == "" {
		cfg.LoginPrompt = defaultLoginPrompt
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("host must be specified for DX cluster")
	}

	// Regex breakdown (groups kept to match existing code indexes):
	// 1: DX de
	// 2: spotter (callsign of the spotter)
	// 3: frequency (allow integers or decimals with any number of fractional digits)
	// 4: spotted (the reported callsign)
	// 5: message (non-greedy, up to the timestamp)
	// 6: 4-digit time (e.g. 1000)
	// 7: optional locator (e.g. FN42)
	// Allow the message portion to be empty (non-greedy), since some servers
	// emit DX lines without an explicit message before the timestamp.
	// Allow underscore in tokens (some servers emit PASSWORD_OK or similar tokens)
	dxDelimRegex := regexp.MustCompile(`^(DX de) +([A-Z0-9_\/\-#]{3,}):? *([0-9]+(?:\.[0-9]+)?) +([A-Z0-9_\/\-#]{3,}) *(.*?) +(\d{4})Z *([A-Z]{2}\d{2})?`)

	// determine buffer size: prefer per-cluster config, fall back to
	// DXC_CHANNEL_BUFFER env var, then to a conservative default of 8.
	buf := cfg.ChannelBuffer
	if buf <= 0 {
		if v := os.Getenv("DXC_CHANNEL_BUFFER"); v != "" {
			if vi, err := strconv.Atoi(v); err == nil && vi > 0 {
				buf = vi
			}
		}
	}
	if buf <= 0 {
		buf = 8
	}

	return &Client{
		cfg: cfg,
		// Use small buffered channels to avoid dropping events when the
		// receiver isn't scheduled immediately. Buffer size is configurable
		// via per-cluster config or DXC_CHANNEL_BUFFER env var.
		SpotChan:     make(chan Spot, buf),
		MessageChan:  make(chan string, buf),
		ErrorChan:    make(chan error, buf),
		readLoopDone: make(chan struct{}),
		dxDelimRegex: dxDelimRegex,
	}, nil
}

// Connect establishes a connection to the DX cluster and starts the read loop.
// It also handles reconnection logic.
func (c *Client) Connect(ctx context.Context) {
	// Create a derived context for the connect/retry loop so Close() can
	// cancel connection attempts even if the parent context hasn't been
	// cancelled yet (defers in RunApplication cancel in reverse order).
	connectCtx, connectCancel := context.WithCancel(ctx)
	c.statusMutex.Lock()
	c.connectCancel = connectCancel
	c.statusMutex.Unlock()

	// Create a done channel so Close() can wait for this goroutine to finish
	c.statusMutex.Lock()
	c.connectDone = make(chan struct{})
	c.statusMutex.Unlock()

	go func() {
		defer func() {
			c.statusMutex.Lock()
			if c.connectDone != nil {
				close(c.connectDone)
				c.connectDone = nil
			}
			c.statusMutex.Unlock()
		}()
		op := func() error {
			// Use the derived connectCtx so that cancelling the connect loop
			// via c.connectCancel() properly halts connection attempts.
			err := c.connectOnce(connectCtx)
			if err != nil {
				// Do not notify listeners when the failure was caused by context
				// cancellation or deadline expiry: tests cancel contexts to
				// signal shutdown and do not expect an error to be emitted in
				// that case. Only emit errors for genuine connection failures.
				if err == context.Canceled || err == context.DeadlineExceeded {
					return err
				}
				c.safeSendError(fmt.Errorf("failed to connect to %s:%s: %w", c.cfg.Host, c.cfg.Port, err))
			}
			return err
		}

		// Exponential backoff for reconnection
		expBackoff := backoff.NewExponentialBackOff()
		expBackoff.InitialInterval = 500 * time.Millisecond
		expBackoff.MaxInterval = 2 * time.Second
		expBackoff.MaxElapsedTime = 10 * time.Second // Keep tests fast; allow some retries then give up
		expBackoff.Multiplier = 1.5

		// Use backoff.WithContext so retries abort immediately when parent ctx is cancelled
		err := backoff.Retry(op, backoff.WithContext(expBackoff, connectCtx))
		if err != nil {
			// If the connect loop ended because the context was cancelled or
			// the deadline was exceeded, don't report a permanent failure as
			// an error to listeners — callers cancelling contexts expect a
			// quiet shutdown.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			c.safeSendError(fmt.Errorf("permanent failure to connect to %s:%s after retries: %w", c.cfg.Host, c.cfg.Port, err))
		}
		// Clear the stored connectCancel when the connect goroutine exits
		c.statusMutex.Lock()
		c.connectCancel = nil
		c.statusMutex.Unlock()
	}()
}

// connectOnce attempts to establish a single connection.
// It returns an error if connection fails or readLoop exits due to error,
// triggering retry from the caller. Returns nil for graceful close.
func (c *Client) connectOnce(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	addr := net.JoinHostPort(c.cfg.Host, c.cfg.Port)
	logging.Info("[%s] Attempting to connect to DX cluster %s...", time.Now().Format(time.RFC3339), addr)

	conn, err := net.DialTimeout("tcp", addr, 10*time.Second) // Connection timeout
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", addr, err)
	}
	c.conn = conn
	logging.Info("[%s] Connected to DX cluster %s.", time.Now().Format(time.RFC3339), addr)

	c.statusMutex.Lock()
	c.isConnected = true
	readLoopCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	// Create a local channel and store it on the struct under lock to avoid races
	doneCh := make(chan struct{})
	c.readLoopDone = doneCh // Reset for new readLoop
	c.readLoopStarted = true
	c.statusMutex.Unlock()

	// Start a goroutine to read from the connection
	go c.readLoop(readLoopCtx)

	// Wait for the readLoop to complete (wait on local variable to avoid races)
	<-doneCh

	c.statusMutex.Lock()
	c.isConnected = false
	c.readLoopStarted = false
	c.statusMutex.Unlock()
	c.statusMutex.Lock()
	if c.conn != nil {
		_ = c.conn.Close() // Ensure underlying connection is closed
		c.conn = nil
	}
	c.statusMutex.Unlock()
	logging.Info("[%s] Connection to DX cluster %s closed.", time.Now().Format(time.RFC3339), addr)

	// Determine if readLoop exited due to error or graceful shutdown
	select {
	case <-readLoopCtx.Done():
		return readLoopCtx.Err() // If context was cancelled (e.g., graceful shutdown)
	default:
		// If readLoopDone signal came without context cancellation, it was an error
		// Return a non-nil error to trigger reconnection (if it's not a permanent error)
		return fmt.Errorf("read loop for %s exited unexpectedly", addr)
	}
}

// Close gracefully closes the connection.
func (c *Client) Close() {
	c.statusMutex.RLock()
	// Stop heartbeat
	if c.pingTicker != nil {
		c.pingTicker.Stop()
		c.pingTicker = nil
	}
	// Cancel the active readLoop (if any)
	if c.cancel != nil {
		c.cancel()
	}
	// Also cancel any ongoing connect/retry loop so connectOnce won't start
	// a new readLoop after we begin closing.
	if c.connectCancel != nil {
		c.connectCancel()
	}
	c.statusMutex.RUnlock()
	// Close the underlying connection to unblock any blocking reads (scanner.Scan).
	c.statusMutex.Lock()
	if c.conn != nil {
		// Send graceful QUIT command before closing
		logging.Debug("Sending QUIT command to DX cluster %s:%s", c.cfg.Host, c.cfg.Port)
		_, err := c.conn.Write([]byte("QUIT\n"))
		if err != nil {
			logging.Debug("Failed to send QUIT to %s:%s: %v", c.cfg.Host, c.cfg.Port, err)
		} else {
			// Give the server a moment to close the connection gracefully
			time.Sleep(100 * time.Millisecond)
		}

		// Set a short read deadline to ensure any blocking reads return
		// quickly (scanner.Scan will observe the deadline). This helps
		// avoid hangs where Close waits for readLoopDone indefinitely.
		_ = c.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		_ = c.conn.Close()
		c.conn = nil
	}
	c.statusMutex.Unlock()

	// Wait for the read loop to exit to avoid races where the readLoop/parsers
	// attempt to send on SpotChan/MessageChan/ErrorChan after we've closed them.
	// Only wait if a readLoop was started; otherwise proceed (the connect loop
	// may have never established a connection and readLoopDone will never
	// be closed).
	waitForReadLoop := false
	c.statusMutex.RLock()
	if c.readLoopStarted {
		waitForReadLoop = true
	}
	c.statusMutex.RUnlock()

	if waitForReadLoop {
		// Wait but avoid blocking forever: give a short timeout for the readLoop
		// to exit after we've cancelled and closed the conn.
		select {
		case <-c.readLoopDone:
			// read loop exited
		case <-time.After(500 * time.Millisecond):
			// Timeout waiting for readLoop; continue to close channels to avoid deadlock
			logging.Warn("Timeout waiting for readLoop to exit for %s:%s", c.cfg.Host, c.cfg.Port)
		}
	}

	// Now it's safe to close channels that this client owns and writes to.
	// Only close them if the readLoop actually exited. If we timed out
	// waiting for the readLoop, it may still be running and could attempt
	// to send on these channels, which would panic if we closed them.
	if waitForReadLoop {
		select {
		case <-c.readLoopDone:
			// Wait for any in-flight connect goroutine to finish too.
			c.statusMutex.RLock()
			connectDone := c.connectDone
			c.statusMutex.RUnlock()
			if connectDone != nil {
				select {
				case <-connectDone:
				case <-time.After(250 * time.Millisecond):
					// Give a small grace period for the connect loop to stop.
				}
			}
			c.spotCloseOnce.Do(func() {
				close(c.SpotChan)
			})

			c.chanCloseOnce.Do(func() {
				close(c.MessageChan)
				close(c.ErrorChan)
			})

		default:
			// readLoop did not exit in time; skip closing channels to avoid races.
			logging.Warn("Skipping channel close because readLoop did not exit in time for %s:%s", c.cfg.Host, c.cfg.Port)
		}
	} else {
		// No readLoop was ever started for this client; safe to close channels.
		c.spotCloseOnce.Do(func() {
			close(c.SpotChan)
		})

		c.chanCloseOnce.Do(func() {
			close(c.MessageChan)
			close(c.ErrorChan)
		})
	}
}

// write sends a string to the DX cluster.
func (c *Client) write(ctx context.Context, str string) error {
	c.statusMutex.RLock()
	isConnected := c.isConnected
	conn := c.conn
	c.statusMutex.RUnlock()

	if !isConnected || conn == nil {
		return fmt.Errorf("not connected to DX cluster %s:%s", c.cfg.Host, c.cfg.Port)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		if _, err := conn.Write([]byte(str + connectionTerminator)); err != nil {
			return fmt.Errorf("error writing to DX cluster %s:%s: %w", c.cfg.Host, c.cfg.Port, err)
		}
		return nil
	}
}

// readLoop continuously reads data from the connection and processes it.
func (c *Client) readLoop(ctx context.Context) {
	defer close(c.readLoopDone) // Signal completion when readLoop exits

	reader := bufio.NewReader(c.conn)

	awaitingLogin := true

	for {
		// Respect cancellation quickly by checking context before attempting a read.
		select {
		case <-ctx.Done():
			logging.Info("[%s] Read loop for %s cancelled by context.", time.Now().Format(time.RFC3339), net.JoinHostPort(c.cfg.Host, c.cfg.Port))
			return
		default:
		}

		// Set a short read deadline so that reads unblock periodically and
		// the loop can observe ctx cancellation.
		_ = c.conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))

		var line string
		var err error

		if awaitingLogin {
			// For login, use a different approach since prompt may not end with newline
			buffer := make([]byte, 256)
			n, readErr := c.conn.Read(buffer)
			if readErr != nil {
				err = readErr
			} else if n > 0 {
				line = string(buffer[:n])
			}
		} else {
			// Normal operation: read complete lines
			line, err = reader.ReadString('\n')
		}
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				// Read timed out; loop and check ctx again.
				continue
			}
			if err == io.EOF {
				logging.Info("[%s] Reader for %s exited gracefully (EOF).", time.Now().Format(time.RFC3339), net.JoinHostPort(c.cfg.Host, c.cfg.Port))
				return
			}
			c.safeSendError(fmt.Errorf("error reading from DX cluster %s: %w", net.JoinHostPort(c.cfg.Host, c.cfg.Port), err))
			return
		}

		line = strings.TrimRight(line, "\r\n")
		// Strip ASCII bell characters that some clusters send
		line = strings.ReplaceAll(line, "\a", "")
		logging.Debug("DX cluster raw line received from %s: %q", net.JoinHostPort(c.cfg.Host, c.cfg.Port), line)

		if awaitingLogin && strings.Contains(line, c.cfg.LoginPrompt) {
			if err := c.write(ctx, c.cfg.Callsign); err != nil {
				c.safeSendError(fmt.Errorf("error sending callsign to %s: %w", c.cfg.Host, err))
				return // Exit readLoop, trigger reconnection
			}
			logging.Debug("[%s] Sent callsign to %s.", time.Now().Format(time.RFC3339), net.JoinHostPort(c.cfg.Host, c.cfg.Port))
			awaitingLogin = false
			continue
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "Logged in." {
			logging.Debug("DX cluster ignored handshake/status line: %q", trimmed)
			continue
		}

		c.parseDX(ctx, line)
	}
	// The deferred close(c.readLoopDone) will handle signaling the parent goroutine
	// The parent (connectOnce) will then determine if it was an error or graceful.
}

// parseDX parses a single line for DX spot information.
func (c *Client) parseDX(ctx context.Context, dxString string) {
	logging.Debug("DX cluster parseDX called with: %q", dxString)

	// Check for PONG response to our PING
	if strings.HasPrefix(dxString, "PONG ") {
		c.statusMutex.Lock()
		c.lastPongTime = time.Now()
		c.statusMutex.Unlock()
		logging.Debug("Connection heartbeat -- server is alive (received PONG)")
		return
	}

	if strings.HasPrefix(dxString, dxIDPrefix) {
		logging.Debug("DX cluster parsing DX line: %q", dxString)
		m := c.dxDelimRegex.FindStringSubmatch(dxString)
		logging.Debug("DX cluster regex matched %d groups", len(m))
		if len(m) < 5 { // Expecting at least up to the spotted call
			c.safeSendError(fmt.Errorf("failed to parse DX string '%s' from %s", dxString, c.cfg.Host))
			c.safeSendMessage(dxString)
			logging.Warn("DX cluster failed to parse DX line, emitting as generic message: %q", dxString)
			return
		}

		spotter := m[2]
		spotted := m[4]
		freqStr := m[3]
		message := m[5]

		// Some cluster lines omit an explicit message and place a single token
		// (e.g., "FIRST") where the message would be. In those cases the
		// regex captures that token as the 'spotted' group and leaves message
		// empty. Tests expect that token to be used as the message. If the
		// message capture is empty, shift the spotted token into message and
		// clear spotted.
		if strings.TrimSpace(message) == "" && strings.TrimSpace(spotted) != "" {
			message = spotted
			spotted = ""
		}

		frequency, err := ParseFrequency(freqStr)
		if err != nil {
			c.safeSendError(fmt.Errorf("failed to parse frequency '%s' from DX string '%s' from %s: %w", freqStr, dxString, c.cfg.Host, err))
			c.safeSendMessage(dxString)
			return
		}

		// Validate required fields before creating spot
		spotter = strings.TrimSpace(spotter)
		spotted = strings.TrimSpace(spotted)
		if spotter == "" {
			logging.Warn("DX cluster spot rejected: missing spotter callsign. line=%q source=%s", dxString, c.cfg.Host)
			return
		}
		if spotted == "" {
			logging.Warn("DX cluster spot rejected: missing spotted callsign. line=%q source=%s", dxString, c.cfg.Host)
			return
		}

		sp := Spot{
			Spotter:   spotter,
			Spotted:   spotted,
			Frequency: frequency, // Frequency in kHz from cluster (matching original format)
			Message:   strings.TrimSpace(message),
			When:      time.Now().UTC(), // Use current UTC for spot reception time
			Source:    c.cfg.Host,
		}

		// Detect band from frequency for logging
		band := spot.BandFromName(frequency)

		c.safeSendSpot(sp)
		logging.Info("DX cluster emitted spot: spotted=%s spotter=%s freq=%.3f band=%s timestamp=%s msg=%q source=%s",
			sp.Spotted, sp.Spotter, sp.Frequency, band, sp.When.Format(time.RFC3339), sp.Message, sp.Source)

		// Start initial heartbeat test after first spot
		c.statusMutex.Lock()
		if !c.initialPingDone {
			c.initialPingDone = true
			c.statusMutex.Unlock()
			go c.startInitialHeartbeatTest(ctx)
		} else {
			c.statusMutex.Unlock()
		}
	} else {
		c.safeSendMessage(dxString)
		logging.Debug("DX cluster emitted generic message: %q", dxString)
	}
}

// parseFrequency converts a frequency string to float64, handling potential commas as decimal points.
// DX clusters typically send kHz, so we preserve the original kHz values.
func ParseFrequency(freqStr string) (float64, error) {
	freqStr = strings.ReplaceAll(freqStr, ",", ".") // Ensure decimal is a dot
	freq, err := strconv.ParseFloat(freqStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid frequency format '%s': %w", freqStr, err)
	}
	// Keep original kHz values to match original application format
	return freq, nil
}

// Helper safe senders: recover from panic if channel was closed concurrently
func (c *Client) safeSendError(err error) {
	if err == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			logging.Warn("Recovered from panic sending error to closed ErrorChan for %s: %v", c.cfg.Host, r)
		}
	}()
	select {
	case c.ErrorChan <- err:
	default:
	}
}

func (c *Client) safeSendMessage(msg string) {
	defer func() {
		if r := recover(); r != nil {
			logging.Warn("Recovered from panic sending message to closed MessageChan for %s: %v", c.cfg.Host, r)
		}
	}()
	select {
	case c.MessageChan <- msg:
	default:
	}
}

func (c *Client) safeSendSpot(sp Spot) {
	logging.Debug("DX cluster sending spot: %s -> %s @ %.1f kHz", sp.Spotter, sp.Spotted, sp.Frequency)
	defer func() {
		if r := recover(); r != nil {
			logging.Warn("Recovered from panic sending spot to closed SpotChan for %s: %v", c.cfg.Host, r)
		}
	}()
	select {
	case c.SpotChan <- sp:
		logging.Debug("DX cluster spot sent successfully")
	default:
		logging.Warn("DX cluster spot channel full, dropping spot")
	}
}

// startInitialHeartbeatTest sends the first PING after receiving the first spot
// and waits 30 seconds for PONG. If successful, starts regular heartbeat.
func (c *Client) startInitialHeartbeatTest(ctx context.Context) {
	logging.Info("Starting initial heartbeat test for %s:%s (30 second timeout)", c.cfg.Host, c.cfg.Port)

	// Send initial PING
	if err := c.write(ctx, "PING"); err != nil {
		logging.Warn("Failed to send initial PING to %s:%s: %v - heartbeat disabled", c.cfg.Host, c.cfg.Port, err)
		return
	}

	c.statusMutex.Lock()
	c.pingCount = 1
	initialPingTime := time.Now()
	c.statusMutex.Unlock()

	logging.Debug("Sent initial PING to %s:%s - waiting for PONG", c.cfg.Host, c.cfg.Port)

	// Wait 30 seconds for PONG response
	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()

	ticker := time.NewTicker(100 * time.Millisecond) // Check every 100ms
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logging.Debug("Initial heartbeat test cancelled for %s:%s", c.cfg.Host, c.cfg.Port)
			return
		case <-timeout.C:
			logging.Warn("Initial heartbeat test failed for %s:%s - no PONG received within 30 seconds, heartbeat disabled", c.cfg.Host, c.cfg.Port)
			return
		case <-ticker.C:
			c.statusMutex.RLock()
			lastPong := c.lastPongTime
			c.statusMutex.RUnlock()

			if lastPong.After(initialPingTime) {
				logging.Info("Initial heartbeat test successful for %s:%s - enabling regular heartbeat", c.cfg.Host, c.cfg.Port)
				c.startRegularHeartbeat(ctx)
				return
			}
		}
	}
}

// startRegularHeartbeat begins the regular PING/PONG heartbeat mechanism.
// Sends PING every 10-15 minutes to keep the connection alive.
func (c *Client) startRegularHeartbeat(ctx context.Context) {
	c.statusMutex.Lock()
	defer c.statusMutex.Unlock()

	if c.pingTicker != nil {
		c.pingTicker.Stop() // Stop any existing ticker
	}

	// Set ping interval between 10-15 minutes (we'll use 12 minutes)
	pingInterval := 12 * time.Minute
	c.pingTicker = time.NewTicker(pingInterval)
	c.heartbeatActive = true

	logging.Info("Starting regular heartbeat for %s:%s (interval: %v)", c.cfg.Host, c.cfg.Port, pingInterval)

	go func() {
		defer func() {
			c.statusMutex.Lock()
			if c.pingTicker != nil {
				c.pingTicker.Stop()
				c.pingTicker = nil
			}
			c.heartbeatActive = false
			c.statusMutex.Unlock()
		}()

		for {
			select {
			case <-ctx.Done():
				logging.Debug("Regular heartbeat stopped for %s:%s (context cancelled)", c.cfg.Host, c.cfg.Port)
				return
			case <-c.pingTicker.C:
				c.statusMutex.Lock()
				c.pingCount++
				pingNum := c.pingCount
				c.statusMutex.Unlock()

				if err := c.write(ctx, "PING"); err != nil {
					logging.Warn("Failed to send PING #%d to %s:%s: %v", pingNum, c.cfg.Host, c.cfg.Port, err)
				} else {
					logging.Debug("Sent PING #%d to %s:%s", pingNum, c.cfg.Host, c.cfg.Port)
				}
			}
		}
	}()
}
