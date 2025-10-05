package pota_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2" // For mocking Redis

	"github.com/user00265/dxclustergoapi/internal/config"
	"github.com/user00265/dxclustergoapi/internal/pota"
	"github.com/user00265/dxclustergoapi/internal/redisclient" // The actual Redis client, wrapped for test
)

// mockHTTPClient for testing external API calls
type mockHTTPClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.DoFunc != nil {
		return m.DoFunc(req)
	}
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
}

// setupTestClient creates a new POTA client with a temporary config.
// It can optionally configure a mock Redis client.
func setupTestClient(t *testing.T, cfg *config.Config, useRedis bool) (*pota.Client, *miniredis.Miniredis, func()) {
	t.Helper()

	var mr *miniredis.Miniredis
	var rdbClient *redisclient.Client

	if useRedis {
		mr = miniredis.NewMiniRedis()
		mr.Start()

		redisCfg := config.RedisConfig{
			Enabled: true,
			Host:    mr.Host(),
			Port:    mr.Port(),
			DB:      0,
		}
		// Temporarily override global config's redis settings for client setup
		cfg.Redis = redisCfg

		// Use a context that gets cancelled on test cleanup
		ctx, cancelCtx := context.WithCancel(context.Background())
		var err error
		rdbClient, err = redisclient.NewClient(ctx, redisCfg)
		if err != nil {
			t.Fatalf("Failed to create mock Redis client: %v", err)
		}

		// Ensure we stop redis and cancel context
		cleanup := func() {
			rdbClient.Close()
			mr.Close()
			cancelCtx()
		}
		return nil, mr, cleanup // Return nil for pota client initially, it's created later
	}

	ctx, cancelCtx := context.WithCancel(context.Background()) // Context for POTA client
	client, err := pota.NewClient(ctx, *cfg, rdbClient)        // rdbClient will be nil if not using Redis
	if err != nil {
		t.Fatalf("Failed to create POTA client: %v", err)
	}

	cleanup := func() {
		client.StopPolling() // Stop scheduler gracefully
		cancelCtx()
	}
	return client, nil, cleanup
}

const testPotaSpotsJSON = `
[
    {
        "spotId": 1,
        "activator": "N1ABC",
        "reference": "K-0001",
        "frequency": 14.285,
        "mode": "SSB",
        "spotter": "W2XYZ",
        "name": "Test Park",
        "locationDesc": "Someplace",
        "spotTime": "2024-01-01T10:00:00Z"
    },
    {
        "spotId": 2,
        "activator": "N1ABC",
        "reference": "K-0001",
        "frequency": 14.286,
        "mode": "SSB",
        "spotter": "W3LMN",
        "name": "Test Park",
        "locationDesc": "Someplace",
        "spotTime": "2024-01-01T10:01:00Z"
    },
    {
        "spotId": 3,
        "activator": "N4DEF",
        "reference": "K-0002",
        "frequency": 7.150,
        "mode": "CW",
        "spotter": "W5OPQ",
        "name": "Another Park",
        "locationDesc": "Elsewhere",
        "spotTime": "2024-01-01T10:02:00Z"
    },
    {
        "spotId": 4,
        "activator": "N4DEF",
        "reference": "K-0002",
        "frequency": 7.074,
        "mode": "FT8",
        "spotter": "W6RST",
        "name": "Another Park",
        "locationDesc": "Elsewhere",
        "spotTime": "2024-01-01T10:03:00Z"
    }
]
`

func TestNewClient_Disabled(t *testing.T) {
	cfg := &config.Config{EnablePOTA: false}
	ctx := context.Background()
	client, err := pota.NewClient(ctx, *cfg, nil) // No Redis
	if err != nil {
		t.Fatalf("NewClient unexpectedly returned an error for disabled POTA: %v", err)
	}
	if client != nil {
		t.Error("NewClient unexpectedly returned a client for disabled POTA")
	}
}

func TestNewClient_SuccessInMemory(t *testing.T) {
	cfg := &config.Config{EnablePOTA: true, POTAPollInterval: 1 * time.Minute, MaxCache: 10}
	client, _, cleanup := setupTestClient(t, cfg, false)
	defer cleanup()

	if client == nil {
		t.Fatal("NewClient returned nil client when POTA enabled")
	}
	if _, ok := client.CacherAccessor().(*pota.InMemorySpotCacher); !ok {
		t.Error("Expected InMemorySpotCacher, got different type")
	}
}

func TestNewClient_SuccessRedis(t *testing.T) {
	cfg := &config.Config{EnablePOTA: true, POTAPollInterval: 1 * time.Minute, MaxCache: 10}
	cfg.Redis.Enabled = true // Enable Redis explicitly

	// Need to initialize redis server first
	mr := miniredis.NewMiniRedis()
	mr.Start()
	defer mr.Close()

	cfg.Redis.Host = mr.Host()
	cfg.Redis.Port = mr.Port()

	ctx, cancelCtx := context.WithCancel(context.Background())
	defer cancelCtx()
	rdbClient, err := redisclient.NewClient(ctx, cfg.Redis)
	if err != nil {
		t.Fatalf("Failed to init mock redis client: %v", err)
	}
	defer rdbClient.Close()

	client, err := pota.NewClient(ctx, *cfg, rdbClient)
	if err != nil {
		t.Fatalf("Failed to create POTA client with Redis: %v", err)
	}
	defer client.StopPolling()

	if client == nil {
		t.Fatal("NewClient returned nil client when POTA and Redis enabled")
	}
	if _, ok := client.CacherAccessor().(*pota.RedisSpotCacher); !ok {
		t.Error("Expected RedisSpotCacher, got different type")
	}
}

func TestFetchAndProcessSpots_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/spot/activator" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(strings.TrimSpace(testPotaSpotsJSON)))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		EnablePOTA:       true,
		POTAPollInterval: 1 * time.Minute,
		MaxCache:         10,
	}
	// Temporarily override global POTAAPIEndpoint for testing
	originalURL := config.POTAAPIEndpoint
	defer func() { config.POTAAPIEndpoint = originalURL }()
	config.POTAAPIEndpoint = server.URL + "/spot/activator"

	client, _, cleanup := setupTestClient(t, cfg, false) // Using in-memory cache
	defer cleanup()

	// Replace the internal httpClient with our mock to hit our test server
	client.HTTPClient = &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("User-Agent") == "" {
				t.Errorf("User-Agent header not set in POTA request")
			}
			return http.DefaultClient.Do(req) // Use default client to hit httptest.Server
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use a WaitGroup to capture spots from the channel
	var (
		receivedSpots []pota.Spot
		wg            sync.WaitGroup
		spotMu        sync.Mutex
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case spot, ok := <-client.SpotChan:
				if !ok {
					return
				}
				spotMu.Lock()
				receivedSpots = append(receivedSpots, spot)
				spotMu.Unlock()
			case <-ctx.Done():
				return
			}
		}
	}()

	// Manually trigger the fetch and process
	client.FetchAndProcessSpots(ctx)

	// Wait a bit for spot processing and channel send
	time.Sleep(50 * time.Millisecond)
	cancel() // Close the context to stop reading from SpotChan and allow wg to finish
	wg.Wait()

	if len(receivedSpots) != 4 {
		t.Fatalf("Expected 4 unique spots, got %d. Spots: %+v", len(receivedSpots), receivedSpots)
	}

	// Verify content of a spot
	firstSpot := receivedSpots[0]
	if firstSpot.Spotted != "N1ABC" || firstSpot.Frequency != 14.285 || firstSpot.Source != "pota" {
		t.Errorf("First spot data mismatch: %+v", firstSpot)
	}
	if firstSpot.AdditionalData.PotaRef != "K-0001" || firstSpot.AdditionalData.PotaMode != "SSB" {
		t.Errorf("First spot additional data mismatch: %+v", firstSpot)
	}
	if !strings.Contains(firstSpot.Message, "POTA @ K-0001 Test Park") {
		t.Errorf("First spot message mismatch: %s", firstSpot.Message)
	}
}

func TestFetchAndProcessSpots_DeduplicationInMemory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(strings.TrimSpace(testPotaSpotsJSON))) // Send same spots repeatedly
	}))
	defer server.Close()

	cfg := &config.Config{
		EnablePOTA:       true,
		POTAPollInterval: 1 * time.Minute,
		MaxCache:         10,
	}
	originalURL := config.POTAAPIEndpoint
	defer func() { config.POTAAPIEndpoint = originalURL }()
	config.POTAAPIEndpoint = server.URL + "/spot/activator"

	client, _, cleanup := setupTestClient(t, cfg, false) // In-memory cache
	defer cleanup()

	client.HTTPClient = &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return http.DefaultClient.Do(req)
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		receivedSpots []pota.Spot
		wg            sync.WaitGroup
		spotMu        sync.Mutex
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case spot, ok := <-client.SpotChan:
				if !ok {
					return
				}
				spotMu.Lock()
				receivedSpots = append(receivedSpots, spot)
				spotMu.Unlock()
			case <-ctx.Done():
				return
			}
		}
	}()

	// First fetch should yield all 4 unique spots
	client.FetchAndProcessSpots(ctx)
	time.Sleep(50 * time.Millisecond)

	// Second fetch of the *same* spots should yield 0 new spots (due to deduplication)
	client.FetchAndProcessSpots(ctx)
	time.Sleep(50 * time.Millisecond)

	cancel() // Stop consumer
	wg.Wait()

	if len(receivedSpots) != 4 {
		t.Fatalf("Expected 4 unique spots after deduplication, got %d. Spots: %+v", len(receivedSpots), receivedSpots)
	}
}

func TestFetchAndProcessSpots_DeduplicationRedis(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(strings.TrimSpace(testPotaSpotsJSON)))
	}))
	defer server.Close()

	cfg := &config.Config{
		EnablePOTA:       true,
		POTAPollInterval: 1 * time.Minute,
		MaxCache:         10,
	}
	cfg.Redis.Enabled = true // Enable Redis explicitly

	mr := miniredis.NewMiniRedis()
	mr.Start()
	defer mr.Close()
	cfg.Redis.Host = mr.Host()
	cfg.Redis.Port = mr.Port()
	cfg.Redis.SpotExpiry = 10 * time.Minute // Longer than test duration

	originalURL := config.POTAAPIEndpoint
	defer func() { config.POTAAPIEndpoint = originalURL }()
	config.POTAAPIEndpoint = server.URL + "/spot/activator"

	ctx, cancelCtx := context.WithCancel(context.Background())
	defer cancelCtx()
	rdbClient, err := redisclient.NewClient(ctx, cfg.Redis)
	if err != nil {
		t.Fatalf("Failed to init mock redis client: %v", err)
	}
	defer rdbClient.Close()

	client, err := pota.NewClient(ctx, *cfg, rdbClient)
	if err != nil {
		t.Fatalf("Failed to create POTA client with Redis: %v", err)
	}
	defer client.StopPolling() // Ensure scheduler stops

	client.HTTPClient = &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return http.DefaultClient.Do(req)
		},
	}

	var (
		receivedSpots []pota.Spot
		wg            sync.WaitGroup
		spotMu        sync.Mutex
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case spot, ok := <-client.SpotChan:
				if !ok {
					return
				}
				spotMu.Lock()
				receivedSpots = append(receivedSpots, spot)
				spotMu.Unlock()
			case <-ctx.Done():
				return
			}
		}
	}()

	// First fetch
	client.FetchAndProcessSpots(ctx)
	time.Sleep(50 * time.Millisecond)

	// Ensure spots were added to miniredis
	if mr.DB(0).Exists("pota:spot:N1ABC:ssb pota @ k-0001 test park someplace:ssb") != true {
		t.Error("Spot not found in Redis after first fetch")
	}

	// Second fetch of same spots
	client.FetchAndProcessSpots(ctx)
	time.Sleep(50 * time.Millisecond)

	cancelCtx() // Stop consumer
	wg.Wait()

	if len(receivedSpots) != 4 {
		t.Fatalf("Expected 4 unique spots after Redis deduplication, got %d. Spots: %+v", len(receivedSpots), receivedSpots)
	}
}

func TestFetchAndProcessSpots_APIError(t *testing.T) {
	cfg := &config.Config{EnablePOTA: true, POTAPollInterval: 1 * time.Minute}
	client, _, cleanup := setupTestClient(t, cfg, false)
	defer cleanup()

	client.HTTPClient = &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("simulated API error")
		},
	}

	ctx := context.Background()
	// FetchAndProcessSpots should log the error but not crash
	client.FetchAndProcessSpots(ctx) // Call directly
	// No spots should be sent to the channel
	select {
	case s := <-client.SpotChan:
		t.Errorf("Unexpected spot received: %+v", s)
	case <-time.After(50 * time.Millisecond):
		// Expected, no spots due to error
	}
}

func TestGetAllowedDeviation(t *testing.T) {
	tests := []struct {
		mode     string
		expected float64
	}{
		{"SSB", 1.0},
		{"CW", 1.0},
		{"FM", 1.0},
		{"FT8", 3.0},
		{"FT4", 3.0},
		{"ft8", 3.0}, // Case insensitive
		{"FT-4", 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			result := pota.GetAllowedDeviation(tt.mode)
			if result != tt.expected {
				t.Errorf("For mode %q, expected deviation %f, got %f", tt.mode, tt.expected, result)
			}
		})
	}
}

func TestRedisSpotCacher_Expiry(t *testing.T) {
	mr := miniredis.NewMiniRedis()
	mr.Start()
	defer mr.Close()

	// cfg not needed for this test
	redisCfg := config.RedisConfig{
		Enabled:    true,
		Host:       mr.Host(),
		Port:       mr.Port(),
		SpotExpiry: 1 * time.Second, // Short expiry for test
	}
	ctx, cancelCtx := context.WithCancel(context.Background())
	defer cancelCtx()

	rdbClient, err := redisclient.NewClient(ctx, redisCfg)
	if err != nil {
		t.Fatalf("Failed to create mock Redis client: %v", err)
	}
	defer rdbClient.Close()

	cacher := pota.NewRedisSpotCacher(rdbClient, redisCfg.SpotExpiry)

	testSpot := pota.Spot{
		Spotter:   "EXPTEST",
		Spotted:   "EXPTEST",
		Frequency: 14.074,
		Message:   "FT8 POTA @ K-EXPY",
	}

	// Add spot to cache
	err = cacher.AddSpotToCache(ctx, testSpot, "FT8")
	if err != nil {
		t.Fatalf("Failed to add spot to cache: %v", err)
	}

	// Should be in cache immediately
	inCache, err := cacher.IsSpotInCache(ctx, testSpot, "FT8")
	if err != nil {
		t.Fatalf("Failed to check cache: %v", err)
	}
	if !inCache {
		t.Error("Expected spot to be in cache immediately")
	}

	// Advance time past expiry
	mr.FastForward(redisCfg.SpotExpiry + 100*time.Millisecond)

	// Should no longer be in cache
	inCache, err = cacher.IsSpotInCache(ctx, testSpot, "FT8")
	if err != nil {
		t.Fatalf("Failed to check cache after expiry: %v", err)
	}
	if inCache {
		t.Error("Expected spot NOT to be in cache after expiry")
	}
}

// Helper to access private field for testing.
// Tests should use exported fields on pota.Client (HTTPClient, Cacher, SpotChan) directly.
