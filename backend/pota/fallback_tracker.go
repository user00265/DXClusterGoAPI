package pota

import (
	"context"
	"sync"
	"time"

	"github.com/user00265/dxclustergoapi/logging"
	"github.com/user00265/dxclustergoapi/redisclient"
	"github.com/user00265/dxclustergoapi/spot"
)

// FallbackSpotTracker wraps a Redis tracker but falls back to in-memory if Redis fails.
// This prevents a single Redis connection loss from blocking POTA spot processing.
// When Redis comes back online, call Flush() to dump accumulated spots back with remaining TTL.
type FallbackSpotTracker struct {
	primary         SpotTracker // Redis tracker (or nil if not available)
	fallback        *InMemorySpotTracker
	mu              sync.RWMutex
	isUsingFallback bool
	lastError       error
	lastErrorTime   time.Time
}

// NewFallbackSpotTracker creates a tracker that falls back from Redis to in-memory.
func NewFallbackSpotTracker(rdb *redisclient.Client, maxCacheSize int, spotExpiry time.Duration) *FallbackSpotTracker {
	fallback := NewInMemorySpotTracker(maxCacheSize)
	fallback.spotExpiry = spotExpiry

	var primary SpotTracker
	if rdb != nil {
		primary = NewRedisSpotTracker(rdb, spotExpiry)
	}

	return &FallbackSpotTracker{
		primary:  primary,
		fallback: fallback,
	}
}

// GetSpot tries Redis first, then falls back to in-memory if needed.
func (f *FallbackSpotTracker) GetSpot(ctx context.Context, sp spot.Spot, mode string) (*spot.Spot, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// If no Redis or we're in fallback mode, use in-memory
	if f.primary == nil || f.isUsingFallback {
		return f.fallback.GetSpot(ctx, sp, mode)
	}

	// Try Redis first
	result, err := f.primary.GetSpot(ctx, sp, mode)
	if err != nil {
		// Log error but return gracefully
		logging.Warn("FallbackSpotTracker.GetSpot failed for Redis: %v. Using fallback.", err)
		f.recordError(err)
		return f.fallback.GetSpot(ctx, sp, mode)
	}

	return result, nil
}

// UpdateSpot updates both Redis (if available) and in-memory fallback.
// In-memory tracker handles TTL expiry automatically during outages.
func (f *FallbackSpotTracker) UpdateSpot(ctx context.Context, sp spot.Spot, mode string) error {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Always update in-memory fallback (which tracks expiry dates)
	fallbackErr := f.fallback.UpdateSpot(ctx, sp, mode)

	// Try to update Redis if available and not in fallback mode
	if f.primary != nil && !f.isUsingFallback {
		if err := f.primary.UpdateSpot(ctx, sp, mode); err != nil {
			logging.Warn("FallbackSpotTracker.UpdateSpot failed for Redis: %v. Using fallback only.", err)
			f.recordError(err)
		}
	}

	return fallbackErr
}

// recordError records the error and potentially triggers fallback mode activation.
func (f *FallbackSpotTracker) recordError(err error) {
	f.lastError = err
	f.lastErrorTime = time.Now()

	// Log activation of fallback mode
	if !f.isUsingFallback {
		logging.Error("FallbackSpotTracker: Redis error detected, activating fallback to in-memory: %v", err)
		f.isUsingFallback = true
	}
}

// IsFallbackActive returns whether the tracker is currently using fallback (for diagnostics).
func (f *FallbackSpotTracker) IsFallbackActive() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.isUsingFallback
}

// LastError returns the last error encountered (for diagnostics).
func (f *FallbackSpotTracker) LastError() (time.Time, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.lastErrorTime, f.lastError
}

// Flush attempts to sync accumulated spots back to Redis with remaining TTL.
// Call this when Redis connection is re-established. The in-memory fallback
// tracker preserves entry times, so we can calculate remaining TTL accurately.
func (f *FallbackSpotTracker) Flush(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.primary == nil || f.fallback == nil {
		return nil
	}

	// Get all entries from in-memory fallback with their remaining TTL
	f.fallback.cacheMutex.RLock()
	entries := make(map[string]struct {
		spot    spot.Spot
		ttlLeft time.Duration
	})

	now := time.Now()
	for k, v := range f.fallback.cache {
		ttlLeft := v.expiry.Sub(now)
		if ttlLeft > 0 {
			entries[k] = struct {
				spot    spot.Spot
				ttlLeft time.Duration
			}{spot: v.spot, ttlLeft: ttlLeft}
		}
	}
	f.fallback.cacheMutex.RUnlock()

	if len(entries) == 0 {
		return nil
	}

	// Sync each entry back to Redis with its remaining TTL
	flushed := 0
	if redisTracker, ok := f.primary.(*RedisSpotTracker); ok {
		oldExpiry := redisTracker.spotExpiry
		for _, entry := range entries {
			// Set Redis tracker to use remaining TTL for this entry
			redisTracker.spotExpiry = entry.ttlLeft
			err := redisTracker.UpdateSpot(ctx, entry.spot, "")
			if err != nil {
				logging.Warn("FallbackSpotTracker.Flush: failed to sync spot: %v", err)
				continue
			}
			flushed++
		}
		redisTracker.spotExpiry = oldExpiry
	}

	if flushed > 0 {
		logging.Notice("FallbackSpotTracker.Flush: synced %d spots back to Redis", flushed)
	}

	return nil
}
