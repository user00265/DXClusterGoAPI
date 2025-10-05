package main_test

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2" // For mocking Redis
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Import main package as test for integration tests
	// mainApp functions (we'll perform lightweight initialization checks instead of running full server)
	mainApp "github.com/user00265/dxclustergoapi/cmd/dxcluster-client"
	"github.com/user00265/dxclustergoapi/internal/config"
	"github.com/user00265/dxclustergoapi/internal/pota"
	"github.com/user00265/dxclustergoapi/internal/spot"
)

// Global constants for test values
const (
	testCallsign        = "TEST0"
	testPassword        = "testpass"
	testWebPort         = 9876
	testBaseURL         = "/api"
	testMaxCache        = 50
	testPotaSpotter     = "POTA1"
	testPotaActivator   = "K2POTA"
	testPotaReference   = "K-0123"
	testPotaFrequency   = 14.300
	testPotaMode        = "SSB"
	testPotaMessage     = "POTA @ K-0123 Test Park"
	testDxSpotter       = "W1AW"
	testDxSpotted       = "K7RA"
	testDxFrequency     = 14.250
	testDxMessage       = "Test Spot Message"
	testDxSource        = "DXFunCluster"
	testSotaClusterName = "SOTACluster"
	testSotaCallsign    = "SOTATEST"
	testSotaFrequency   = 7.030
	testSotaMessage     = "SOTA Summit"
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
			s.listener.Close() // Ensure listener is closed if context is cancelled
			return
		default:
			if tl, ok := s.listener.(*net.TCPListener); ok {
				tl.SetDeadline(time.Now().Add(100 * time.Millisecond))
			}
			conn, err := s.listener.Accept()
			if err != nil {
				if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
					continue // Timeout, check context again
				}
				if strings.Contains(err.Error(), "use of closed network connection") || strings.Contains(err.Error(), "bad file descriptor") {
					// Expected errors on shutdown
				} else {
					fmt.Printf("Mock server accept error: %v\n", err) // For debugging
				}
				return // Listener closed or other fatal error
			}
			s.mu.Lock()
			s.conns = append(s.conns, conn)
			s.mu.Unlock()

			s.wg.Add(1) // Add to waitgroup for this connection's handler
			go func(c net.Conn) {
				defer s.wg.Done()
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
	s.wg.Wait() // Wait for all goroutines (run() and handlers) to exit

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

// dxClusterHandler simulates DX cluster auth and sends a test spot.
func dxClusterHandler(conn net.Conn, cfg config.ClusterConfig) {
	defer conn.Close()
	reader := bufio.NewReader(conn)

	// Login
	conn.Write([]byte(fmt.Sprintf("%s\n", cfg.LoginPrompt)))
	line, _, err := reader.ReadLine()
	if err != nil || strings.TrimSpace(string(line)) != cfg.Callsign {
		return
	}

	// Password (if configured)
	if cfg.Password != "" {
		conn.Write([]byte("password:\n"))
		line, _, err = reader.ReadLine()
		if err != nil || strings.TrimSpace(string(line)) != cfg.Password {
			return
		}
	}
	conn.Write([]byte("Logged in.\n"))
	time.Sleep(10 * time.Millisecond) // Give client time to process login

	// Send a test spot
	conn.Write([]byte(fmt.Sprintf("DX de %s: %.3f %s %s %sZ FN42\n", testDxSpotter, testDxFrequency, testDxSpotted, testDxMessage, time.Now().UTC().Format("1504"))))
	conn.Write([]byte(fmt.Sprintf("DX de %s: %.3f %s %s %sZ FN42\n", testDxSpotter, testDxFrequency+0.001, testDxSpotted, testDxMessage+"2", time.Now().UTC().Format("1504")))) // A second spot
	// Send SOTA-relevant spot
	conn.Write([]byte(fmt.Sprintf("DX de %s: %.3f %s SOTA Ref: SOTA-ABC %sZ FN42\n", testDxSpotter, testSotaFrequency, testSotaCallsign, time.Now().UTC().Format("1504"))))

	// Give the client a short moment to process the test lines, then close the connection.
	// Keeping the connection open indefinitely can make shutdown nondeterministic in tests.
	time.Sleep(50 * time.Millisecond)
}

// mockPotaAPIHandler simulates the POTA API.
func mockPotaAPIHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/spot/activator" {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]pota.PotaRawSpot{
			{
				Activator:    testPotaActivator,
				Reference:    testPotaReference,
				Frequency:    testPotaFrequency,
				Mode:         testPotaMode,
				Spotter:      testPotaSpotter,
				Name:         "Test Park",
				LocationDesc: "Someplace",
				SpotTime:     time.Now().UTC(),
			},
		})
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// mockClubLogAPIHandler simulates the Club Log API.
func mockClubLogAPIHandler(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.URL.Path, "/cty.php") {
		// Respond with a minimal gzipped XML for testing
		xmlContent := `<?xml version="1.0" encoding="ISO-8859-1"?>
<cty>
  <prefixes><prefix record="1"><call>K</call><entity>United States</entity><adif>291</adif><cqz>5</cqz><cont>NA</cont><long>-98.0</long><lat>39.0</lat></prefix></prefixes>
  <exceptions><exception record="1"><call>KG4</call><entity>Guantanamo Bay</entity><adif>105</adif><cqz>8</cqz><cont>NA</cont><long>-75.1</long><lat>19.9</lat></exception></exceptions>
  <entities><entity adif="0"><name>- NONE -</name><prefix></prefix><ituz>0</ituz><cqz>0</cqz><cont></cont><long>0.0</long><lat>0.0</lat></entity><entity adif="291"><name>United States</name><prefix>K</prefix><ituz>8</ituz><cqz>5</cqz><cont>NA</cont><long>-98.0</long><lat>39.0</lat></entity><entity adif="105"><name>Guantanamo Bay</name><prefix>KG4</prefix><ituz>11</ituz><cqz>8</cqz><cont>NA</cont><long>-75.1</long><lat>19.9</lat></entity></entities>
</cty>`
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		gz.Write([]byte(xmlContent))
		gz.Close()
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		w.Write(buf.Bytes())
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// mockLoTWAPIHandler simulates the LoTW API.
func mockLoTWAPIHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/lotw-user-activity.csv" {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, fmt.Sprintf("%s,%s,%s\n", testDxSpotted, time.Now().Add(-24*time.Hour).Format("2006-01-02"), time.Now().Add(-24*time.Hour).Format("15:04:05")))
		io.WriteString(w, fmt.Sprintf("%s,%s,%s\n", testPotaActivator, time.Now().Add(-48*time.Hour).Format("2006-01-02"), time.Now().Add(-48*time.Hour).Format("15:04:05")))
		io.WriteString(w, fmt.Sprintf("%s,%s,%s\n", testSotaCallsign, time.Now().Add(-72*time.Hour).Format("2006-01-02"), time.Now().Add(-72*time.Hour).Format("15:04:05")))
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestMainIntegration(t *testing.T) {
	// 1. Setup mock external services
	// DX Cluster 1
	dxCluster1, err := newMockDXClusterServer(func(conn net.Conn) {
		dxClusterHandler(conn, config.ClusterConfig{
			Callsign:    testCallsign,
			Password:    testPassword,
			LoginPrompt: "Please enter your call:",
			ClusterName: testDxSource,
		})
	})
	require.NoError(t, err)
	defer dxCluster1.Close()
	dx1Host, dx1Port, _ := net.SplitHostPort(dxCluster1.Addr())
	// Wait for the mock server to report it's running to avoid races with
	// the client connecting immediately after RunApplication starts.
	waitForMockServer(t, dxCluster1)

	// SOTA Cluster (another DX Cluster)
	sotaCluster, err := newMockDXClusterServer(func(conn net.Conn) {
		dxClusterHandler(conn, config.ClusterConfig{
			Callsign:    testCallsign, // Same callsign for simplicity
			LoginPrompt: "Please enter your call:",
			ClusterName: testSotaClusterName,
		})
	})
	require.NoError(t, err)
	defer sotaCluster.Close()
	sotaHost, sotaPort, _ := net.SplitHostPort(sotaCluster.Addr())
	waitForMockServer(t, sotaCluster)

	// POTA API Mock
	potaServer := httptest.NewServer(http.HandlerFunc(mockPotaAPIHandler))
	defer potaServer.Close()

	// ClubLog API Mock
	clubLogServer := httptest.NewServer(http.HandlerFunc(mockClubLogAPIHandler))
	defer clubLogServer.Close()

	// LoTW API Mock
	lotwServer := httptest.NewServer(http.HandlerFunc(mockLoTWAPIHandler))
	defer lotwServer.Close()

	// Redis Mock
	mr := miniredis.NewMiniRedis()
	mr.Start()
	defer mr.Close()

	// 2. Set environment variables for the application
	// Store original env vars to restore them later
	originalEnvs := make(map[string]string)
	for _, key := range []string{
		"WEBPORT", "WEBURL", "MAXCACHE", "DATA_DIR", "CLUSTERS",
		"POTA_INTEGRATION", "POTA_POLLING_INTERVAL",
		"CLUBLOG_API_KEY", "LOTW_UPDATE_INTERVAL",
		"REDIS_ENABLED", "REDIS_HOST", "REDIS_PORT", "REDIS_PASSWORD", "REDIS_DB", "REDIS_SPOT_EXPIRY",
	} {
		if val, ok := os.LookupEnv(key); ok {
			originalEnvs[key] = val
		}
	}
	// Restore env vars at the end of the test
	defer func() {
		for key, val := range originalEnvs {
			os.Setenv(key, val)
		}
	}()

	// Choose an ephemeral free port for the HTTP server to avoid intermittent
	// "address already in use" failures on CI or when tests are run in parallel.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	_, portStr, _ := net.SplitHostPort(addr)
	l.Close()
	os.Setenv("WEBPORT", portStr)
	os.Setenv("WEBURL", testBaseURL)
	os.Setenv("MAXCACHE", fmt.Sprintf("%d", testMaxCache))
	os.Setenv("DATA_DIR", t.TempDir()) // Use a temporary directory for SQLite
	os.Setenv("CLUSTERS", fmt.Sprintf(`[
		{"host":"%s","port":"%s","call":"%s","password":"%s","loginPrompt":"Please enter your call:","cluster":"%s"},
		{"host":"%s","port":"%s","call":"%s","loginPrompt":"Please enter your call:","cluster":"%s"}
	]`, dx1Host, dx1Port, testCallsign, testPassword, testDxSource, sotaHost, sotaPort, testCallsign, testSotaClusterName))
	os.Setenv("POTA_INTEGRATION", "true")
	os.Setenv("POTA_POLLING_INTERVAL", "1s") // Fast polling for test
	os.Setenv("CLUBLOG_API_KEY", "dummykey")
	os.Setenv("LOTW_UPDATE_INTERVAL", "1s") // Fast update for test
	os.Setenv("REDIS_ENABLED", "true")
	os.Setenv("REDIS_HOST", mr.Host())
	os.Setenv("REDIS_PORT", mr.Port())
	os.Setenv("REDIS_PASSWORD", "")
	os.Setenv("REDIS_DB", "0")
	os.Setenv("REDIS_SPOT_EXPIRY", "5s")

	// Enable test-only debug API for deterministic assertions
	os.Setenv("DX_API_TEST_DEBUG", "1")

	// Override global URLs for mocked servers
	originalPotaAPIEndpoint := config.POTAAPIEndpoint
	originalClubLogAPIURL := config.ClubLogAPIURL
	originalLoTWActivityURL := config.LoTWActivityURL
	defer func() {
		config.POTAAPIEndpoint = originalPotaAPIEndpoint
		config.ClubLogAPIURL = originalClubLogAPIURL
		config.LoTWActivityURL = originalLoTWActivityURL
	}()
	config.POTAAPIEndpoint = potaServer.URL + "/spot/activator"
	config.ClubLogAPIURL = clubLogServer.URL + "/cty.php?api=%s"
	config.LoTWActivityURL = lotwServer.URL + "/lotw-user-activity.csv"

	// Test /healthcheck subcommand directly (not HTTP)
	// Run this before starting the long-running "serve" instance to avoid
	// port bind races when RunApplication is invoked twice in-process.
	t.Run("healthcheck_cmd_success", func(t *testing.T) {
		// Replace log output temporarily to capture healthcheck output without clutter
		oldLogOutput := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		// Call the application's RunApplication with "healthcheck" subcommand
		// It needs to parse config, connect to Redis/DBs etc.
		// For a real healthcheck, it should also check core app services.
		// For simplicity, this test checks that the healthcheck command itself runs without fatal errors.
		healthCtx, healthCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer healthCancel()
		status := mainApp.RunApplication(healthCtx, []string{"healthcheck"})
		assert.Equal(t, 0, status, "Healthcheck subcommand should exit with 0 for success")

		w.Close()
		out, _ := io.ReadAll(r)
		os.Stdout = oldLogOutput // Restore log output
		assert.Contains(t, string(out), "Health check successful", "Healthcheck should report success")
	})

	// 3. Run the main application in a goroutine
	appCtx, appCancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Call the application's RunApplication with "serve" subcommand
		mainApp.RunApplication(appCtx, []string{"serve"})
	}()

	// Give the application some time to start up, connect, and fetch initial data
	time.Sleep(3 * time.Second)

	// 4. Test HTTP API endpoints
	client := &http.Client{Timeout: 5 * time.Second}
	// Use the ephemeral port selected earlier (stored in portStr) so tests
	// target the actual server instance we started, not the hard-coded
	// `testWebPort` constant.
	baseURL := fmt.Sprintf("http://localhost:%s%s", portStr, testBaseURL)

	// Test /spots
	t.Run("/spots", func(t *testing.T) {
		// The application ingests spots asynchronously (DX clusters, POTA, LoTW).
		// Poll for a short window until we observe spots to make the test
		// robust against timing differences on CI or slow machines.
		spots, err := fetchSpotsWithRetry(client, fmt.Sprintf("%s/spots", baseURL), 2*time.Second)
		require.NoError(t, err)
		require.True(t, len(spots) > 0, "Expected to find some spots")

		// Check for enriched data in a spot
		foundDxSpotEnriched := false
		for _, s := range spots {
			if s.Source == testDxSource && s.Spotted == testDxSpotted {
				foundDxSpotEnriched = true
				assert.Equal(t, "NA", s.SpottedInfo.DXCC.Cont)
				assert.Equal(t, "United States", s.SpottedInfo.DXCC.Entity)
				assert.Equal(t, "\U0001F1FA\U0001F1F8", s.SpottedInfo.DXCC.Flag)
				assert.True(t, s.SpottedInfo.IsLoTWUser, "Expected %s to be LoTW user", s.Spotted)
				assert.Equal(t, "20m", s.Band)
				break
			}
		}
		assert.True(t, foundDxSpotEnriched, "Expected to find at least one enriched DX spot")

		// Check for POTA spot
		foundPota := false
		for _, s := range spots {
			if s.Source == "pota" && s.Spotted == testPotaActivator {
				foundPota = true
				assert.Equal(t, testPotaReference, s.AdditionalData.PotaRef)
				assert.Equal(t, testPotaMode, s.AdditionalData.PotaMode)
				assert.True(t, s.SpottedInfo.IsLoTWUser, "Expected POTA activator to be LoTW user")
				break
			}
		}
		if !foundPota {
			// Dump spots for debugging timing/content issues
			b, _ := json.MarshalIndent(spots, "", "  ")
			t.Logf("/spots returned (no POTA found): %s", string(b))
		}
		assert.True(t, foundPota, "Expected to find at least one POTA spot")

		// Check for SOTA spot from cluster
		foundSota := false
		for _, s := range spots {
			if s.Source == testSotaClusterName && s.Spotted == testSotaCallsign {
				foundSota = true
				assert.Equal(t, "40m", s.Band) // Based on 7.030 MHz
				assert.True(t, s.SpottedInfo.IsLoTWUser, "Expected SOTA activator to be LoTW user")
				break
			}
		}
		assert.True(t, foundSota, "Expected to find at least one SOTA spot from cluster")
	})

	// Test /spot/:qrg
	t.Run("/spot/:qrg", func(t *testing.T) {
		resp, err := client.Get(fmt.Sprintf("%s/spot/%.3f", baseURL, testDxFrequency))
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var s spot.Spot
		err = json.NewDecoder(resp.Body).Decode(&s)
		require.NoError(t, err)
		assert.Equal(t, testDxSpotted, s.Spotted)
		assert.Equal(t, testDxFrequency, s.Frequency)
	})

	// Test /spots/:band
	t.Run("/spots/:band", func(t *testing.T) {
		resp, err := client.Get(fmt.Sprintf("%s/spots/20m", baseURL))
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		var spots []spot.Spot
		err = json.NewDecoder(resp.Body).Decode(&spots)
		require.NoError(t, err)
		assert.True(t, len(spots) > 0, "Expected 20m spots")
		assert.True(t, spots[0].Band == "20m", "Expected band 20m")
	})

	// Test /spots/source/:source
	t.Run("/spots/source/:source", func(t *testing.T) {
		spots, err := fetchSpotsWithRetry(client, fmt.Sprintf("%s/spots/source/pota", baseURL), 2*time.Second)
		require.NoError(t, err)
		require.True(t, len(spots) > 0, "Expected POTA spots")
		assert.Equal(t, "pota", spots[0].Source)
	})

	// Test /spots/callsign/:callsign
	t.Run("/spots/callsign/:callsign", func(t *testing.T) {
		resp, err := client.Get(fmt.Sprintf("%s/spots/callsign/%s", baseURL, testDxSpotted))
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		var spots []spot.Spot
		err = json.NewDecoder(resp.Body).Decode(&spots)
		require.NoError(t, err)
		assert.True(t, len(spots) > 0, "Expected spots for K7RA")
		assert.True(t, spots[0].Spotted == testDxSpotted || spots[0].Spotter == testDxSpotted)
	})

	// Test /spotter/:callsign
	t.Run("/spotter/:callsign", func(t *testing.T) {
		resp, err := client.Get(fmt.Sprintf("%s/spotter/%s", baseURL, testDxSpotter))
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		var spots []spot.Spot
		err = json.NewDecoder(resp.Body).Decode(&spots)
		require.NoError(t, err)
		assert.True(t, len(spots) > 0, "Expected spots from W1AW")
		assert.Equal(t, testDxSpotter, spots[0].Spotter)
	})

	// Test /spotted/:callsign
	t.Run("/spotted/:callsign", func(t *testing.T) {
		resp, err := client.Get(fmt.Sprintf("%s/spotted/%s", baseURL, testDxSpotted))
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		var spots []spot.Spot
		err = json.NewDecoder(resp.Body).Decode(&spots)
		require.NoError(t, err)
		assert.True(t, len(spots) > 0, "Expected spots for K7RA")
		assert.Equal(t, testDxSpotted, spots[0].Spotted)
	})

	// Test /spots/dxcc/:dxcc
	t.Run("/spots/dxcc/:dxcc", func(t *testing.T) {
		resp, err := client.Get(fmt.Sprintf("%s/spots/dxcc/United States", baseURL))
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		var spots []spot.Spot
		err = json.NewDecoder(resp.Body).Decode(&spots)
		require.NoError(t, err)
		require.True(t, len(spots) > 0, "Expected spots for United States DXCC")

		// DXCC enrichment may happen asynchronously (DXCC updater can reload
		// data after spots are already aggregated). To avoid races, assert
		// that at least one spot in the result has the expected DXCC entity
		// rather than relying on spots[0]. This makes the test robust on CI.
		found := false
		for _, s := range spots {
			if s.SpottedInfo.DXCC.Entity == "United States" {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected at least one spot with DXCC Entity 'United States'")
	})

	// Test /stats
	t.Run("/stats", func(t *testing.T) {
		resp, err := client.Get(fmt.Sprintf("%s/stats", baseURL))
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var stats map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&stats)
		require.NoError(t, err)
		assert.True(t, stats["entries"].(float64) > 0, "Expected entries in stats")
		assert.True(t, stats["cluster"].(float64) > 0, "Expected cluster spots in stats")
		assert.True(t, stats["pota"].(float64) > 0, "Expected POTA spots in stats")
		assert.NotEmpty(t, stats["freshest"], "Expected freshest timestamp")
		assert.NotEmpty(t, stats["oldest"], "Expected oldest timestamp")
	})

	// Test /debug (test-only endpoint)
	t.Run("/debug", func(t *testing.T) {
		url := fmt.Sprintf("%s/debug", baseURL)
		counts, err := fetchDebugCountsWithRetry(client, url, 2*time.Second)
		require.NoError(t, err)
		// Keys are lowercased by the server
		potaKey := "pota"
		dxKey := strings.ToLower(testDxSource)
		sotaKey := strings.ToLower(testSotaClusterName)
		assert.True(t, counts[potaKey] > 0, "Expected POTA count > 0 in /api/debug")
		assert.True(t, counts[dxKey] > 0, "Expected cluster (%s) count > 0 in /api/debug", dxKey)
		assert.True(t, counts[sotaKey] > 0, "Expected SOTA cluster (%s) count > 0 in /api/debug", sotaKey)
	})

	// 5. Signal application to shut down
	appCancel()
	wg.Wait() // Wait for main goroutine to exit
}

// waitForMockServer polls the mock server until it reports running or times out.
func waitForMockServer(t *testing.T, s *mockDXClusterServer) {
	deadline := time.After(2 * time.Second)
	for {
		if s.IsRunning() {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("mock server %s failed to start", s.Addr())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// fetchSpotsWithRetry continuously GETs the provided URL until a non-empty
// slice of spots is returned or the timeout elapses. It returns the last
// successful decoded slice or an error.
func fetchSpotsWithRetry(client *http.Client, url string, timeout time.Duration) ([]spot.Spot, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			time.Sleep(25 * time.Millisecond)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("unexpected status: %d", resp.StatusCode)
			resp.Body.Close()
			time.Sleep(25 * time.Millisecond)
			continue
		}
		var spots []spot.Spot
		err = json.NewDecoder(resp.Body).Decode(&spots)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			time.Sleep(25 * time.Millisecond)
			continue
		}
		if len(spots) > 0 {
			return spots, nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no spots observed before timeout")
}

// fetchDebugCountsWithRetry polls the /debug endpoint until a non-empty
// per_source map is returned or timeout elapses.
func fetchDebugCountsWithRetry(client *http.Client, url string, timeout time.Duration) (map[string]int, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			time.Sleep(25 * time.Millisecond)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("unexpected status: %d", resp.StatusCode)
			resp.Body.Close()
			time.Sleep(25 * time.Millisecond)
			continue
		}
		var payload struct {
			PerSource map[string]int `json:"per_source"`
		}
		err = json.NewDecoder(resp.Body).Decode(&payload)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			time.Sleep(25 * time.Millisecond)
			continue
		}
		if len(payload.PerSource) > 0 {
			return payload.PerSource, nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no per_source counts observed before timeout")
}
