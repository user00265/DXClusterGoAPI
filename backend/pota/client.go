package pota

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	// using a simple ticker for polling instead of gocron to avoid API mismatches
	"github.com/go-redis/redis/v8" // For Redis client operations (SETEX, GET)

	"github.com/user00265/dxclustergoapi/config"
	"github.com/user00265/dxclustergoapi/logging"
	"github.com/user00265/dxclustergoapi/redisclient"
	"github.com/user00265/dxclustergoapi/spot"
	"github.com/user00265/dxclustergoapi/utils"
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
	Frequency      int64     `json:"frequency"` // In Hz (canonical storage unit)
	Message        string    `json:"message"`
	When           time.Time `json:"when"`
	Source         string    `json:"source"` // "pota"
	AdditionalData struct {
		PotaRef  string `json:"pota_ref"`
		PotaMode string `json:"pota_mode"`
	} `json:"additional_data"`
}

// SpotTracker defines the interface for tracking POTA spots with expiry management.
type SpotTracker interface {
	GetSpot(ctx context.Context, sp spot.Spot, mode string) (*spot.Spot, error)
	UpdateSpot(ctx context.Context, sp spot.Spot, mode string) error
	// ClearCache() error // Not strictly needed with expiration, but useful for testing or explicit resets
}

// HTTPDoer is a minimal interface for http clients used in tests.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// spotCacheEntry represents a cached spot with an expiry time
type spotCacheEntry struct {
	spot   spot.Spot
	expiry time.Time
}

// InMemorySpotTracker implements SpotTracker using an in-memory map with expiry tracking.
type InMemorySpotTracker struct {
	cache        map[string]spotCacheEntry // Key: "spotted:message:mode"
	cacheMutex   sync.RWMutex
	maxCacheSize int           // Max spots to keep in memory
	spotExpiry   time.Duration // How long to keep spots before expiring
}

// NewInMemorySpotTracker creates a new in-memory spot tracker.
func NewInMemorySpotTracker(maxSize int) *InMemorySpotTracker {
	return &InMemorySpotTracker{
		cache:        make(map[string]spotCacheEntry),
		maxCacheSize: maxSize,
		spotExpiry:   10 * time.Minute, // Default 10 minutes to match Redis behavior
	}
}

// GetSpot retrieves a spot from the tracker if it exists and hasn't expired.
func (c *InMemorySpotTracker) GetSpot(ctx context.Context, newSpot spot.Spot, mode string) (*spot.Spot, error) {
	c.cacheMutex.RLock()
	defer c.cacheMutex.RUnlock()

	key := fmt.Sprintf("%s:%s:%s", newSpot.Spotted, strings.ToLower(newSpot.Message), strings.ToLower(mode))
	entry, exists := c.cache[key]
	if !exists {
		return nil, nil
	}

	// Check if entry has expired
	if time.Now().After(entry.expiry) {
		return nil, nil
	}

	return &entry.spot, nil
}

// UpdateSpot adds or updates a spot in the in-memory tracker with expiry tracking.
func (c *InMemorySpotTracker) UpdateSpot(ctx context.Context, sp spot.Spot, mode string) error {
	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()

	key := fmt.Sprintf("%s:%s:%s", sp.Spotted, strings.ToLower(sp.Message), strings.ToLower(mode))

	// Update or insert the spot with new expiry time
	c.cache[key] = spotCacheEntry{
		spot:   sp,
		expiry: time.Now().Add(c.spotExpiry),
	}

	logging.Debug("POTA add/update in-memory tracker: key=%s freq=%s expiry=%v", key, utils.FormatFrequency(sp.Frequency), c.spotExpiry)

	// Clean up expired entries if cache is getting large
	if len(c.cache) > c.maxCacheSize*2 { // Clean up when we exceed 2x max size
		now := time.Now()
		for k, v := range c.cache {
			if now.After(v.expiry) {
				delete(c.cache, k)
			}
		}
	}

	// If still too large, remove oldest entries
	if len(c.cache) > c.maxCacheSize {
		// Find entries with earliest expiry and remove them
		type expEntry struct {
			key    string
			expiry time.Time
		}
		entries := make([]expEntry, 0, len(c.cache))
		for k, v := range c.cache {
			entries = append(entries, expEntry{k, v.expiry})
		}
		// Sort by expiry time (ascending)
		for i := 0; i < len(entries)-1; i++ {
			for j := i + 1; j < len(entries); j++ {
				if entries[j].expiry.Before(entries[i].expiry) {
					entries[i], entries[j] = entries[j], entries[i]
				}
			}
		}
		// Remove oldest entries until we're under maxCacheSize
		removeCount := len(c.cache) - c.maxCacheSize
		for i := 0; i < removeCount && i < len(entries); i++ {
			delete(c.cache, entries[i].key)
		}
	}

	return nil
}

// RedisSpotTracker implements SpotTracker using Redis.
type RedisSpotTracker struct {
	rdb        *redisclient.Client
	spotExpiry time.Duration
}

// NewRedisSpotTracker creates a new Redis-backed spot tracker.
func NewRedisSpotTracker(rdb *redisclient.Client, spotExpiry time.Duration) *RedisSpotTracker {
	return &RedisSpotTracker{
		rdb:        rdb,
		spotExpiry: spotExpiry,
	}
}

// GetSpot retrieves a spot from Redis, if it exists and hasn't expired.
func (c *RedisSpotTracker) GetSpot(ctx context.Context, newSpot spot.Spot, mode string) (*spot.Spot, error) {
	// Generate a base key for the spot
	baseKey := fmt.Sprintf("pota:spot:%s:%s:%s", newSpot.Spotted, strings.ToLower(newSpot.Message), strings.ToLower(mode))

	cachedSpotJSON, err := c.rdb.Get(ctx, baseKey).Result()
	if err == redis.Nil {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get spot from Redis: %w", err)
	}

	// Support stored value being either a single Spot JSON or an array of Spots.
	var storedSpot spot.Spot
	if err := json.Unmarshal([]byte(cachedSpotJSON), &storedSpot); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cached spot JSON: %w", err)
	}

	return &storedSpot, nil
}

// UpdateSpot updates or inserts a spot in Redis with TTL expiry.
func (c *RedisSpotTracker) UpdateSpot(ctx context.Context, sp spot.Spot, mode string) error {
	baseKey := fmt.Sprintf("pota:spot:%s:%s:%s", sp.Spotted, strings.ToLower(sp.Message), strings.ToLower(mode))

	spotJSON, err := json.Marshal(sp)
	if err != nil {
		return fmt.Errorf("failed to marshal spot for Redis: %w", err)
	}

	logging.Debug("POTA update Redis tracker: key=%s freq=%s expiry=%v", baseKey, utils.FormatFrequency(sp.Frequency), c.spotExpiry)
	return c.rdb.SetEX(ctx, baseKey, spotJSON, c.spotExpiry).Err()
}

// Old IsSpotInCache checks if a spot exists in Redis, using a composite key and frequency deviation.
// (Deprecated - no longer used for filtering, but kept for reference)
func (c *RedisSpotTracker) isSpotInCacheOld(ctx context.Context, newSpot spot.Spot, mode string) (bool, error) {
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
	var existingSpots []spot.Spot
	if err := json.Unmarshal([]byte(cachedSpotJSON), &existingSpots); err != nil {
		// Try single Spot
		var single spot.Spot
		if err2 := json.Unmarshal([]byte(cachedSpotJSON), &single); err2 != nil {
			return false, fmt.Errorf("failed to unmarshal cached spot JSON (both array and single): %w / %v", err, err2)
		}
		existingSpots = []spot.Spot{single}
	}

	allowedDeviation := getAllowedDeviation(mode)
	toleranceHz := int64(allowedDeviation * 1000) // Convert kHz to Hz
	for _, existingSpot := range existingSpots {
		if strings.EqualFold(existingSpot.Spotted, newSpot.Spotted) && strings.EqualFold(existingSpot.Message, newSpot.Message) {
			if utils.FrequencyDeviation(newSpot.Frequency, existingSpot.Frequency, toleranceHz) {
				logging.Debug("POTA dedupe check (redis): spotted=%s msg=%q existingFreq=%s newFreq=%s within tolerance=%d Hz",
					newSpot.Spotted, newSpot.Message, utils.FormatFrequency(existingSpot.Frequency), utils.FormatFrequency(newSpot.Frequency), toleranceHz)
				return true, nil
			}
		}
	}

	return false, nil
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
	tracker  SpotTracker
	SpotChan chan spot.Spot // Output channel for new POTA spots
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
		SpotChan: make(chan spot.Spot, 8),
	}

	client.HTTPClient = client.httpClient

	// Choose tracking mechanism with fallback support
	if cfg.Redis.Enabled {
		// Use FallbackSpotTracker even if rdb is nil (graceful degradation)
		client.tracker = NewFallbackSpotTracker(rdb, cfg.MaxCache, cfg.Redis.SpotExpiry)
		if rdb != nil {
			logging.Notice("POTA client using Redis tracker with fallback to in-memory (expiry: %s)", cfg.Redis.SpotExpiry)
		} else {
			logging.Warn("POTA client: Redis enabled but not available. Using in-memory tracker with fallback support.")
		}
	} else {
		client.tracker = NewInMemorySpotTracker(cfg.MaxCache) // Use MaxCache as a proxy for in-memory size
		logging.Info("POTA client using in-memory tracker (max %d spots).", cfg.MaxCache)
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
	logging.Notice("POTA polling started. Initial poll will run shortly, then every %s.", c.cfg.POTAPollInterval)
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
	req, err := http.NewRequestWithContext(ctx, "GET", config.POTAAPIEndpoint, nil)
	if err != nil {
		logging.Error("Failed to create HTTP request for POTA API: %v", err)
		return
	}
	req.Header.Set("User-Agent", version.UserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logging.Error("HTTP request to POTA API failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logging.Error("POTA API returned non-OK status: %s", resp.Status)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logging.Error("Failed to read POTA response body: %v", err)
		return
	}

	var rawSpots []PotaRawSpot
	if err := json.Unmarshal(body, &rawSpots); err != nil {
		logging.Error("Failed to unmarshal POTA API response: %v", err)
		return
	}
	logging.Info("Received %d POTA spots", len(rawSpots))

	for _, item := range rawSpots {
		// Parse frequency from string to Hz using utils
		freqHz, err := utils.ParseFrequency(item.Frequency)
		if err != nil || freqHz == 0 {
			logging.Warn("POTA received spot with invalid frequency '%s': %v", item.Frequency, err)
			continue
		}

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

		// cleanCallsign removes invalid characters and suffixes that interfere with DXCC lookup
		cleanCallsign := func(call string) string {
			call = strings.TrimSpace(call)
			// Remove common POTA system suffixes that aren't part of the actual callsign
			if idx := strings.Index(call, "-#"); idx != -1 {
				call = call[:idx]
			}
			return strings.TrimSpace(call)
		}

		// Validate required fields before creating spot
		spotter := cleanCallsign(item.Spotter)
		activator := cleanCallsign(item.Activator)
		if spotter == "" || activator == "" {
			logging.Warn("POTA spot rejected: missing spotter=%q or activator=%q ref=%s freq=%s", spotter, activator, item.Reference, utils.FormatFrequency(freqHz))
			continue // Skip this spot
		}

		potaSpot := Spot{
			Spotter:   spotter,
			Spotted:   activator,
			Frequency: freqHz,
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

		logging.Debug("POTA received spot: activator=%s spotter=%s freq=%s mode=%s ref=%s", activator, spotter, utils.FormatFrequency(freqHz), item.Mode, item.Reference)
		// Use the unified canonical spot for all processing
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

		// Always emit the spot. If a location is re-spotted (e.g., frequency change),
		// we emit the updated spot and update the cache accordingly.
		logging.Info("POTA emitting spot: spotted=%s freq=%s msg=%q", potaSpot.Spotted, utils.FormatFrequency(potaSpot.Frequency), potaSpot.Message)

		// Send the unified, cleaned spot so downstream consumers and JSON
		// marshaling only ever see the canonical values (no '-#').
		select {
		case c.SpotChan <- unified:
		case <-ctx.Done():
			// If context is canceled, don't block; drop the spot
		}

		// Update the tracker with the current spot (replaces previous entry if it exists).
		// This allows re-spotted activations to be re-emitted if frequency or other details change.
		if err := c.tracker.UpdateSpot(ctx, unified, item.Mode); err != nil {
			logging.Error("Failed to update POTA spot tracker: %v", err)
		}
	}
	logging.Debug("POTA spot processing complete.")
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

// TrackerAccessor exposes the tracker for tests.
func (c *Client) TrackerAccessor() SpotTracker {
	return c.tracker
}
