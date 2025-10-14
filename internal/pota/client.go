package pota

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	// using a simple ticker for polling instead of gocron to avoid API mismatches
	"github.com/go-redis/redis/v8" // For Redis client operations (SETEX, GET)

	"github.com/user00265/dxclustergoapi/internal/config"
	"github.com/user00265/dxclustergoapi/internal/logging"
	"github.com/user00265/dxclustergoapi/internal/redisclient"
	"github.com/user00265/dxclustergoapi/internal/spot"
	"github.com/user00265/dxclustergoapi/version" // For User-Agent
)

const (
	apiTimeout = 10 * time.Second
)

// PotaRawSpot represents the structure of a raw spot from the POTA API.
type PotaRawSpot struct {
	SpotID       int     `json:"spotId"`
	Activator    string  `json:"activator"`
	Reference    string  `json:"reference"`
	Frequency    string  `json:"frequency"` // API returns as string (e.g., "14005.3")
	Mode         string  `json:"mode"`
	Spotter      string  `json:"spotter"`
	Name         string  `json:"name"`
	LocationDesc string  `json:"locationDesc"`
	SpotTime     string  `json:"spotTime"` // API returns as string "2025-10-14T09:20:33" (no timezone)
	Comments     string  `json:"comments"`
	Source       string  `json:"source"` // "Web", "RBN", etc.
	Grid4        string  `json:"grid4"`
	Grid6        string  `json:"grid6"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	Count        int     `json:"count"`
	Expire       int     `json:"expire"`   // Expiry time in seconds
	Invalid      *string `json:"invalid"`  // Can be null
	ParkName     *string `json:"parkName"` // Can be null
}

// Spot represents a parsed POTA spot, to be consistent with DXCluster Spot.
type Spot struct {
	Spotter        string    `json:"spotter"`
	Spotted        string    `json:"spotted"`
	Frequency      float64   `json:"frequency"` // In MHz
	Message        string    `json:"message"`
	When           time.Time `json:"when"`
	Source         string    `json:"source"` // "pota"
	AdditionalData struct {
		PotaRef  string `json:"pota_ref"`
		PotaMode string `json:"pota_mode"`
	} `json:"additional_data"`
}

// SpotCacher defines the interface for caching POTA spots to prevent re-emitting duplicates.
type SpotCacher interface {
	IsSpotInCache(ctx context.Context, spot Spot, mode string) (bool, error)
	AddSpotToCache(ctx context.Context, spot Spot, mode string) error
	// ClearCache() error // Not strictly needed with expiration, but useful for testing or explicit resets
}

// HTTPDoer is a minimal interface for http clients used in tests.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// InMemorySpotCacher implements SpotCacher using an in-memory slice.
type InMemorySpotCacher struct {
	cache        []Spot
	cacheMutex   sync.RWMutex
	maxCacheSize int // Max spots to keep in memory (similar to MAXCACHE)
}

// NewInMemorySpotCacher creates a new in-memory cache.
func NewInMemorySpotCacher(maxSize int) *InMemorySpotCacher {
	return &InMemorySpotCacher{
		cache:        make([]Spot, 0, maxSize),
		maxCacheSize: maxSize,
	}
}

// IsSpotInCache checks if a spot (excluding "when") exists in the cache,
// considering frequency deviation. This logic mimics the original Node.js code.
func (c *InMemorySpotCacher) IsSpotInCache(ctx context.Context, newSpot Spot, mode string) (bool, error) {
	c.cacheMutex.RLock()
	defer c.cacheMutex.RUnlock()

	allowedDeviation := getAllowedDeviation(mode)
	for _, existingSpot := range c.cache {
		if existingSpot.Spotted == newSpot.Spotted && strings.EqualFold(existingSpot.Message, newSpot.Message) {
			// allowedDeviation returned in kHz; convert to MHz for comparison
			allowedMHz := allowedDeviation / 1000.0
			diff := math.Abs(newSpot.Frequency - existingSpot.Frequency)
			// Debug print to help tests understand floating point rounding behavior
			logging.Debug("POTA dedupe check (in-memory): spotted=%s msg=%q existingFreq=%f newFreq=%f diff=%f allowedMHz=%f",
				newSpot.Spotted, newSpot.Message, existingSpot.Frequency, newSpot.Frequency, diff, allowedMHz)
			// Consider a spot a duplicate only if the frequency difference is
			// strictly less than the allowed deviation. Use a tiny epsilon to
			// avoid floating point rounding causing near-equal values to compare
			// as smaller than the threshold.
			const eps = 1e-9
			if diff < allowedMHz-eps {
				return true, nil
			}
		}
	}
	return false, nil
}

// AddSpotToCache adds a spot to the in-memory cache, managing its size.
func (c *InMemorySpotCacher) AddSpotToCache(ctx context.Context, spot Spot, mode string) error {
	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()

	// Append new spot
	c.cache = append(c.cache, spot)

	logging.Debug("POTA add to in-memory cache: spotted=%s msg=%q freq=%f", spot.Spotted, spot.Message, spot.Frequency)

	// Trim cache if it exceeds max size, keeping the freshest (last) spots
	if len(c.cache) > c.maxCacheSize {
		c.cache = c.cache[len(c.cache)-c.maxCacheSize:]
	}
	return nil
}

// RedisSpotCacher implements SpotCacher using Redis.
type RedisSpotCacher struct {
	rdb        *redisclient.Client
	spotExpiry time.Duration
}

// NewRedisSpotCacher creates a new Redis-backed cache.
func NewRedisSpotCacher(rdb *redisclient.Client, spotExpiry time.Duration) *RedisSpotCacher {
	return &RedisSpotCacher{
		rdb:        rdb,
		spotExpiry: spotExpiry,
	}
}

// IsSpotInCache checks if a spot exists in Redis, using a composite key and frequency deviation.
func (c *RedisSpotCacher) IsSpotInCache(ctx context.Context, newSpot Spot, mode string) (bool, error) {
	// Generate a base key for the spot (excluding frequency)
	baseKey := fmt.Sprintf("pota:spot:%s:%s:%s", newSpot.Spotted, strings.ToLower(newSpot.Message), strings.ToLower(mode))

	// For frequency deviation, we can store a set of frequencies for a base key
	// Or, more simply, store a single frequency, and check if the new one falls within deviation.
	// For now, let's keep it simple: store a string representation that includes frequency for comparison.
	// A more robust solution might involve geo-spatial indices if frequency deviation was truly a range check.
	// For simple "seen this exact spot (within freq deviation) recently" check, we can store a canonical key.

	// For a more exact match as in Node.js, we'll retrieve the stored spot data.
	// We'll store the full spot data as JSON string in Redis.
	cachedSpotJSON, err := c.rdb.Get(ctx, baseKey).Result()
	if err == redis.Nil {
		return false, nil // Not found
	}
	if err != nil {
		return false, fmt.Errorf("failed to get spot from Redis: %w", err)
	}

	// Support stored value being either a single Spot JSON or an array of Spots.
	var existingSpots []Spot
	if err := json.Unmarshal([]byte(cachedSpotJSON), &existingSpots); err != nil {
		// Try single Spot
		var single Spot
		if err2 := json.Unmarshal([]byte(cachedSpotJSON), &single); err2 != nil {
			return false, fmt.Errorf("failed to unmarshal cached spot JSON (both array and single): %w / %v", err, err2)
		}
		existingSpots = []Spot{single}
	}

	allowedDeviation := getAllowedDeviation(mode)
	allowedMHz := allowedDeviation / 1000.0
	for _, existingSpot := range existingSpots {
		if strings.EqualFold(existingSpot.Spotted, newSpot.Spotted) && strings.EqualFold(existingSpot.Message, newSpot.Message) {
			diff := math.Abs(newSpot.Frequency - existingSpot.Frequency)
			logging.Debug("POTA dedupe check (redis): spotted=%s msg=%q existingFreq=%f newFreq=%f diff=%f allowedMHz=%f",
				newSpot.Spotted, newSpot.Message, existingSpot.Frequency, existingSpot.Frequency, diff, allowedMHz)
			const eps = 1e-9
			if diff < allowedMHz-eps {
				return true, nil
			}
		}
	}

	return false, nil
}

// AddSpotToCache adds a spot to Redis with a TTL.
func (c *RedisSpotCacher) AddSpotToCache(ctx context.Context, spot Spot, mode string) error {
	baseKey := fmt.Sprintf("pota:spot:%s:%s:%s", spot.Spotted, strings.ToLower(spot.Message), strings.ToLower(mode))
	// Use SetEX to set with expiry
	// If an existing entry exists, append to array so we keep multiple nearby frequencies
	existingJSON, err := c.rdb.Get(ctx, baseKey).Result()
	if err == redis.Nil {
		// No existing value, store as single-element array for consistency
		arr := []Spot{spot}
		out, _ := json.Marshal(arr)
		logging.Debug("POTA add to redis cache (new): key=%s freq=%f", baseKey, spot.Frequency)
		return c.rdb.SetEX(ctx, baseKey, out, c.spotExpiry).Err()
	}
	if err != nil {
		return fmt.Errorf("failed to get existing redis spot for append: %w", err)
	}

	var spots []Spot
	if err := json.Unmarshal([]byte(existingJSON), &spots); err != nil {
		// Try single Spot value
		var single Spot
		if err2 := json.Unmarshal([]byte(existingJSON), &single); err2 != nil {
			// fallback: overwrite with new array containing both serialized values
			spots = []Spot{spot}
		} else {
			spots = []Spot{single, spot}
		}
	} else {
		spots = append(spots, spot)
	}

	out, _ := json.Marshal(spots)
	logging.Debug("POTA add to redis cache (append): key=%s freq=%f total=%d", baseKey, spot.Frequency, len(spots))
	return c.rdb.SetEX(ctx, baseKey, out, c.spotExpiry).Err()
}

// Client manages POTA API polling and spot processing.
type Client struct {
	cfg        config.Config
	httpClient *http.Client
	// Exposed for tests
	HTTPClient HTTPDoer
	// polling control
	pollStop chan struct{}
	pollDone chan struct{}
	cacher   SpotCacher
	SpotChan chan Spot // Output channel for new, unique POTA spots
}

// NewClient creates and returns a new POTA client.
func NewClient(ctx context.Context, cfg config.Config, rdb *redisclient.Client) (*Client, error) {
	if !cfg.EnablePOTA {
		return nil, nil // POTA is disabled
	}

	client := &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: apiTimeout,
		},
		// Buffer the output channel so that an initial immediate poll can
		// emit spots before downstream consumers (forwarders) are wired up.
		SpotChan: make(chan Spot, 8),
	}

	client.HTTPClient = client.httpClient

	// Choose caching mechanism
	if cfg.Redis.Enabled && rdb != nil {
		client.cacher = NewRedisSpotCacher(rdb, cfg.Redis.SpotExpiry)
		logging.Info("POTA client using Redis cache with expiry: %s", cfg.Redis.SpotExpiry)
	} else {
		client.cacher = NewInMemorySpotCacher(cfg.MaxCache) // Use MaxCache as a proxy for in-memory size
		logging.Info("POTA client using in-memory cache (max %d spots).", cfg.MaxCache)
	}

	// polling will be started via StartPolling
	client.pollStop = nil
	client.pollDone = nil

	return client, nil
}

// StartPolling begins the continuous polling of the POTA API.
func (c *Client) StartPolling(ctx context.Context) {
	if c == nil {
		return // POTA client not initialized if disabled
	}
	// Start a ticker-based polling loop
	if c.pollStop != nil {
		return // already running
	}
	c.pollStop = make(chan struct{})
	c.pollDone = make(chan struct{})

	// Immediate run
	go c.fetchAndProcessSpots(ctx)

	go func() {
		ticker := time.NewTicker(c.cfg.POTAPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.fetchAndProcessSpots(ctx)
			case <-c.pollStop:
				close(c.pollDone)
				return
			case <-ctx.Done():
				close(c.pollDone)
				return
			}
		}
	}()
	logging.Info("POTA polling started. Initial poll will run shortly, then every %s.", c.cfg.POTAPollInterval)
}

// StopPolling halts the continuous polling.
func (c *Client) StopPolling() {
	if c == nil {
		return // POTA client not initialized if disabled
	}
	if c.pollStop != nil {
		close(c.pollStop)
		<-c.pollDone
		c.pollStop = nil
		c.pollDone = nil
	}
	close(c.SpotChan) // Signal no more spots
	logging.Info("POTA polling stopped.")
}

// fetchAndProcessSpots fetches, parses, and caches POTA spots.
func (c *Client) fetchAndProcessSpots(ctx context.Context) {
	logging.Info("[%s] Fetching POTA spots from %s", time.Now().Format(time.RFC3339), config.POTAAPIEndpoint)
	req, err := http.NewRequestWithContext(ctx, "GET", config.POTAAPIEndpoint, nil)
	if err != nil {
		logging.Error("[%s] Failed to create HTTP request for POTA API: %v", time.Now().Format(time.RFC3339), err)
		return
	}
	req.Header.Set("User-Agent", version.UserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logging.Error("[%s] HTTP request to POTA API failed: %v", time.Now().Format(time.RFC3339), err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logging.Error("[%s] POTA API returned non-OK status: %s", time.Now().Format(time.RFC3339), resp.Status)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logging.Error("[%s] Failed to read POTA response body: %v", time.Now().Format(time.RFC3339), err)
		return
	}

	var rawSpots []PotaRawSpot
	if err := json.Unmarshal(body, &rawSpots); err != nil {
		logging.Error("[%s] Failed to unmarshal POTA API response: %v", time.Now().Format(time.RFC3339), err)
		return
	}
	logging.Debug("[%s] Received %d raw POTA spots.", time.Now().Format(time.RFC3339), len(rawSpots))

	for _, item := range rawSpots {
		// Parse frequency from string to float64
		freq, err := strconv.ParseFloat(item.Frequency, 64)
		if err != nil || freq == 0 {
			logging.Warn("POTA received spot with invalid frequency '%s': %v", item.Frequency, err)
			continue
		}

		// POTA API returns frequency in kHz, convert to MHz
		freqMHz := freq / 1000.0

		// Parse spot time from string to time.Time
		spotTime, err := time.Parse("2006-01-02T15:04:05", item.SpotTime)
		if err != nil {
			logging.Warn("POTA received spot with invalid time '%s': %v", item.SpotTime, err)
			// Use current time as fallback
			spotTime = time.Now().UTC()
		} else {
			// Assume UTC since no timezone is provided by POTA API
			spotTime = spotTime.UTC()
		}

		// Validate required fields before creating spot
		spotter := strings.TrimSpace(item.Spotter)
		activator := strings.TrimSpace(item.Activator)
		if spotter == "" || activator == "" {
			logging.Warn("POTA spot rejected: missing spotter=%q or activator=%q ref=%s freq=%.1f", spotter, activator, item.Reference, freq)
			continue // Skip this spot
		}

		potaSpot := Spot{
			Spotter:   spotter,
			Spotted:   activator,
			Frequency: freqMHz,
			// Build message without extra parentheses to match tests' expected canonicalization
			Message: fmt.Sprintf("%s%sPOTA @ %s %s %s", item.Mode, func() string {
				if item.Mode != "" {
					return " "
				}
				return ""
			}(), item.Reference, item.Name, item.LocationDesc),
			When:   spotTime,
			Source: "pota",
		}
		potaSpot.AdditionalData.PotaRef = item.Reference
		potaSpot.AdditionalData.PotaMode = item.Mode

		logging.Debug("POTA received raw spot: activator=%s spotter=%s freq=%.1f mode=%s ref=%s", item.Activator, item.Spotter, freq, item.Mode, item.Reference)
		isNewSpot, err := c.cacher.IsSpotInCache(ctx, potaSpot, item.Mode)
		if err != nil {
			logging.Error("[%s] Failed to check POTA spot cache: %v", time.Now().Format(time.RFC3339), err)
			continue
		}

		if !isNewSpot {
			logging.Info("POTA emitting spot: spotted=%s freq=%f msg=%q", potaSpot.Spotted, potaSpot.Frequency, potaSpot.Message)
			// Convert to unified spot.Spot for downstream consumers
			unified := spot.Spot{
				Spotter:   potaSpot.Spotter,
				Spotted:   potaSpot.Spotted,
				Frequency: potaSpot.Frequency,
				Message:   potaSpot.Message,
				When:      potaSpot.When,
				Source:    potaSpot.Source,
			}
			unified.AdditionalData.PotaRef = potaSpot.AdditionalData.PotaRef
			unified.AdditionalData.PotaMode = potaSpot.AdditionalData.PotaMode

			// Send package-local POTA Spot type so tests and consumers receive the expected type
			c.SpotChan <- potaSpot
			if err := c.cacher.AddSpotToCache(ctx, potaSpot, item.Mode); err != nil {
				logging.Error("[%s] Failed to add POTA spot to cache: %v", time.Now().Format(time.RFC3339), err)
			}
		}
	}
	logging.Debug("[%s] POTA spot processing complete.", time.Now().Format(time.RFC3339))
}

// getAllowedDeviation determines the allowed frequency deviation based on mode.
func getAllowedDeviation(mode string) float64 {
	switch strings.ToUpper(mode) {
	case "FT8", "FT4":
		return 3.0 // kHz units expected by tests
	default:
		return 1.0 // kHz units expected by tests
	}
}

// GetAllowedDeviation is exported for tests.
func GetAllowedDeviation(mode string) float64 {
	return getAllowedDeviation(mode)
}

// FetchAndProcessSpots exposes the internal fetch for tests.
func (c *Client) FetchAndProcessSpots(ctx context.Context) {
	c.fetchAndProcessSpots(ctx)
}

// CacherAccessor exposes the cacher for tests.
func (c *Client) CacherAccessor() SpotCacher {
	return c.cacher
}
