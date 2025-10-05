package cluster_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/user00265/dxclustergoapi/internal/cluster"
	"github.com/user00265/dxclustergoapi/internal/config"
)

// mockDXClusterServer is a simple TCP server to simulate a DX cluster.
type mockDXClusterServer struct {
	listener net.Listener
	addr     string
	mu       sync.Mutex
	conns    []net.Conn
	handler  func(conn net.Conn)
	wg       sync.WaitGroup
	running  bool
	shutdown context.CancelFunc
}

func newMockDXClusterServer(handler func(conn net.Conn)) (*mockDXClusterServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0") // Listen on a random available port
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &mockDXClusterServer{
		listener: listener,
		addr:     listener.Addr().String(),
		handler:  handler,
		shutdown: cancel,
	}
	s.wg.Add(1)
	go s.run(ctx)
	return s, nil
}

func (s *mockDXClusterServer) run(ctx context.Context) {
	defer s.wg.Done()
	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// net.Listener does not guarantee SetDeadline; cast to *net.TCPListener when available.
			if tl, ok := s.listener.(*net.TCPListener); ok {
				tl.SetDeadline(time.Now().Add(100 * time.Millisecond))
			}
			conn, err := s.listener.Accept()
			if err != nil {
				if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
					continue // Timeout, check context again
				}
				if !strings.Contains(err.Error(), "use of closed network connection") {
					// fmt.Printf("Mock server accept error: %v\n", err) // For debugging
				}
				return // Listener closed or other fatal error
			}
			s.mu.Lock()
			s.conns = append(s.conns, conn)
			s.mu.Unlock()

			go func(c net.Conn) {
				s.handler(c)
				s.removeConn(c)
			}(conn)
		}
	}
}

func (s *mockDXClusterServer) Addr() string {
	return s.addr
}

func (s *mockDXClusterServer) Close() {
	s.shutdown() // Signal run() to stop
	s.listener.Close()
	s.wg.Wait() // Wait for run() to exit

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, conn := range s.conns {
		conn.Close() // Close all active connections
	}
	s.conns = nil
	s.running = false
}

func (s *mockDXClusterServer) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *mockDXClusterServer) removeConn(target net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, conn := range s.conns {
		if conn == target {
			s.conns = append(s.conns[:i], s.conns[i+1:]...)
			conn.Close()
			return
		}
	}
}

// simpleAuthHandler simulates a DX cluster that asks for callsign and sends a spot.
func simpleAuthHandler(conn net.Conn, callsign string) {
	defer conn.Close()
	buffer := bufio.NewReader(conn)

	// Send login prompt
	conn.Write([]byte("Please enter your call:\n"))

	// Expect callsign
	line, _, err := buffer.ReadLine()
	if err != nil {
		return
	}
	if strings.TrimSpace(string(line)) != callsign {
		conn.Write([]byte("Invalid callsign.\n"))
		return
	}
	conn.Write([]byte("Logged in.\n"))
	time.Sleep(10 * time.Millisecond) // Give client time to process login
	// Send a test spot
	conn.Write([]byte("DX de W1AW: 14.250 K7RA Test Message 1000Z FN42\n"))
	conn.Write([]byte("This is a generic message.\n"))
	conn.Write([]byte("DX de AA1ZZ: 7.050 KC0AAA Another Spot 1005Z EN37\n"))

	// Keep connection open or close immediately after sending data
	// For testing readLoop, it should stay open until client closes or server dies
	// Use a long sleep or block on read to simulate open connection
	select {} // Block indefinitely
}

func TestNewClient_Success(t *testing.T) {
	cfg := config.ClusterConfig{
		Host:     "localhost", // Dummy, will be set by mock server
		Port:     "7300",
		Callsign: "TESTCALL",
	}

	client, err := cluster.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if client == nil {
		t.Fatal("NewClient returned nil client")
	}
}

func TestNewClient_MissingCallsign(t *testing.T) {
	cfg := config.ClusterConfig{
		Host: "localhost",
		Port: "7300",
		// Callsign omitted
	}

	_, err := cluster.NewClient(cfg)
	if err == nil {
		t.Error("Expected error for missing callsign, got nil")
	}
	if !strings.Contains(err.Error(), "callsign must be specified") {
		t.Errorf("Expected 'callsign must be specified' error, got: %v", err)
	}
}

func TestClient_ConnectAndParse(t *testing.T) {
	expectedCallsign := "TESTCALL"
	mockHandler := func(conn net.Conn) {
		simpleAuthHandler(conn, expectedCallsign)
	}
	server, err := newMockDXClusterServer(mockHandler)
	if err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	defer server.Close()

	host, port, _ := net.SplitHostPort(server.Addr())
	cfg := config.ClusterConfig{
		Host:        host,
		Port:        port,
		Callsign:    expectedCallsign,
		LoginPrompt: "Please enter your call:",
		ClusterName: "MockCluster",
	}

	client, err := cluster.NewClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client.Connect(ctx) // Start connection in background

	// Collect spots and messages
	var (
		receivedSpots    []cluster.Spot
		receivedMessages []string
		receivedErrors   []error
		wg               sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case spot := <-client.SpotChan:
				receivedSpots = append(receivedSpots, spot)
			case msg := <-client.MessageChan:
				receivedMessages = append(receivedMessages, msg)
			case err := <-client.ErrorChan:
				receivedErrors = append(receivedErrors, err)
			case <-time.After(3 * time.Second): // Timeout to stop collecting
				return
			}
		}
	}()

	// Wait for spots/messages to be received
	time.Sleep(1 * time.Second) // Give client time to connect and process
	cancel()                    // Signal client to close gracefully

	wg.Wait() // Wait for collection goroutine to finish

	if len(receivedErrors) > 0 {
		t.Errorf("Received unexpected errors: %+v", receivedErrors)
	}
	if len(receivedSpots) != 2 {
		t.Fatalf("Expected 2 spots, got %d. Spots: %+v", len(receivedSpots), receivedSpots)
	}
	if receivedSpots[0].Spotted != "K7RA" || receivedSpots[0].Frequency != 14.250 {
		t.Errorf("First spot mismatch: %+v", receivedSpots[0])
	}
	if receivedSpots[1].Spotted != "KC0AAA" || receivedSpots[1].Frequency != 7.050 {
		t.Errorf("Second spot mismatch: %+v", receivedSpots[1])
	}
	if len(receivedMessages) != 1 {
		t.Errorf("Expected 1 generic message, got %d. Messages: %+v", len(receivedMessages), receivedMessages)
	}
	if !strings.Contains(receivedMessages[0], "This is a generic message.") {
		t.Errorf("Generic message mismatch: %s", receivedMessages[0])
	}
}

func TestClient_ReconnectOnServerError(t *testing.T) {
	expectedCallsign := "TESTCALL"
	callCount := 0
	serverFactory := func() *mockDXClusterServer {
		handler := func(conn net.Conn) {
			defer conn.Close()
			buffer := bufio.NewReader(conn)
			conn.Write([]byte("Please enter your call:\n"))
			buffer.ReadLine() // Read callsign
			conn.Write([]byte("Logged in.\n"))
			time.Sleep(50 * time.Millisecond)
			if callCount == 0 {
				conn.Write([]byte("DX de W1AW: 14.000 FIRST 1000Z\n"))
				callCount++
				// First connection closes immediately to simulate server error
			} else {
				conn.Write([]byte("DX de W1AW: 14.000 SECOND 1000Z\n"))
				// Keep second connection open
				select {}
			}
		}
		s, _ := newMockDXClusterServer(handler)
		return s
	}

	server1 := serverFactory()
	defer server1.Close() // Will be closed when test ends
	host, port, _ := net.SplitHostPort(server1.Addr())

	cfg := config.ClusterConfig{
		Host:        host,
		Port:        port,
		Callsign:    expectedCallsign,
		LoginPrompt: "Please enter your call:",
		ClusterName: "ReconnectTest",
	}

	client, err := cluster.NewClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client.Connect(ctx)

	var receivedSpots []cluster.Spot
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case spot := <-client.SpotChan:
				receivedSpots = append(receivedSpots, spot)
			case err := <-client.ErrorChan:
				t.Logf("Received error (expected for reconnect): %v", err) // Log expected error
			case <-time.After(5 * time.Second): // Give enough time for reconnect
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	time.Sleep(100 * time.Millisecond) // Give first connection time to establish and close

	// Give time for client to attempt reconnection and get second spot
	time.Sleep(3 * time.Second)

	cancel()
	wg.Wait()

	// Check if both spots were received from successful first and then reconnected second attempts
	if len(receivedSpots) != 2 {
		t.Fatalf("Expected 2 spots after reconnect, got %d: %+v", len(receivedSpots), receivedSpots)
	}
	if receivedSpots[0].Message != "FIRST" || receivedSpots[1].Message != "SECOND" {
		t.Errorf("Spots not in expected order or content: %+v", receivedSpots)
	}
}

func TestClient_PasswordAuth(t *testing.T) {
	expectedCallsign := "TESTCALL"
	expectedPassword := "testpass"

	mockHandler := func(conn net.Conn) {
		defer conn.Close()
		buffer := bufio.NewReader(conn)

		conn.Write([]byte("Please enter your call:\n"))
		line, _, err := buffer.ReadLine()
		if err != nil || strings.TrimSpace(string(line)) != expectedCallsign {
			return
		}

		conn.Write([]byte("password:\n")) // Password prompt
		line, _, err = buffer.ReadLine()
		if err != nil || strings.TrimSpace(string(line)) != expectedPassword {
			return
		}
		conn.Write([]byte("Logged in.\n"))
		conn.Write([]byte("DX de W1AW: 14.000 PASSWORD_OK 1000Z\n"))
		select {}
	}
	server, err := newMockDXClusterServer(mockHandler)
	if err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	defer server.Close()

	host, port, _ := net.SplitHostPort(server.Addr())
	cfg := config.ClusterConfig{
		Host:        host,
		Port:        port,
		Callsign:    expectedCallsign,
		Password:    expectedPassword,
		LoginPrompt: "Please enter your call:",
		ClusterName: "PassTest",
	}

	client, err := cluster.NewClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client.Connect(ctx)

	var receivedSpots []cluster.Spot
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case spot := <-client.SpotChan:
				receivedSpots = append(receivedSpots, spot)
			case <-time.After(2 * time.Second):
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	time.Sleep(1 * time.Second) // Give time for connection and auth
	cancel()
	wg.Wait()

	if len(receivedSpots) != 1 || receivedSpots[0].Message != "PASSWORD_OK" {
		t.Errorf("Expected 1 spot after password auth, got %+v", receivedSpots)
	}
}

func TestClient_ConnectionTimeout(t *testing.T) {
	// Don't start a server, client should time out
	cfg := config.ClusterConfig{
		Host:        "127.0.0.1",
		Port:        "9999", // Unused port
		Callsign:    "TIMEOUT",
		LoginPrompt: "Please enter your call:",
	}
	client, err := cluster.NewClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client.Connect(ctx) // This will retry, so we need to capture the first connectOnce error

	var receivedErrors []error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case err := <-client.ErrorChan:
				receivedErrors = append(receivedErrors, err)
			case <-time.After(5 * time.Second): // Give time for retries
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	time.Sleep(4 * time.Second) // Allow some retries
	cancel()
	wg.Wait()

	if len(receivedErrors) == 0 {
		t.Error("Expected connection timeout errors, got none")
	}
	if !strings.Contains(receivedErrors[0].Error(), "failed to connect") {
		t.Errorf("Expected connection error, got: %v", receivedErrors[0])
	}
}

func TestParseFrequency(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
		err      bool
	}{
		{"14.250", 14.250, false},
		{"7,050", 7.050, false}, // Comma replaced with dot
		{"28.12345", 28.12345, false},
		{"100", 100.0, false},
		{"invalid", 0, true},
		{"14.foo", 0, true},
		{"", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			freq, err := cluster.ParseFrequency(tt.input)
			if (err != nil) != tt.err {
				t.Fatalf("ParseFrequency(%q) error status mismatch. Expected error: %t, got: %v", tt.input, tt.err, err)
			}
			if !tt.err && freq != tt.expected {
				t.Errorf("ParseFrequency(%q) expected %f, got %f", tt.input, tt.expected, freq)
			}
		})
	}
}
