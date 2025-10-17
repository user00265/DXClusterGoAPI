package frontend

import (
	"sort"
	"sync"

	"github.com/user00265/dxclustergoapi/spot"
)

// Cache manages a thread-safe collection of DX spots with size limiting and timestamp ordering.
// If Redis is enabled, this acts as the "source of truth" after aggregation,
// and API queries will still filter this memory cache.
// For persistence + direct Redis query, we'd need a different strategy.
// For now, mirroring Node.js: in-memory is the primary API source,
// Redis for POTA dedupe.
type Cache struct {
	sync.RWMutex
	spots   []spot.Spot
	maxSize int
}

// NewCache creates a new empty spot cache with default max size.
func NewCache() *Cache {
	return &Cache{
		spots:   make([]spot.Spot, 0, 1000),
		maxSize: 1000,
	}
}

// AddSpot adds or updates a spot in the cache.
// If a spot with the same spotted callsign and frequency exists, it is updated (not duplicated).
// If cache exceeds max size, oldest spots are removed.
// Returns true if the spot was updated, false if it was added as new.
func (c *Cache) AddSpot(newSpot spot.Spot) bool {
	c.Lock()
	defer c.Unlock()

	// Look for existing spot with same spotted callsign, frequency, and source
	// This forms the unique key: (spotted, frequency, source)
	for i := range c.spots {
		if c.spots[i].Spotted == newSpot.Spotted &&
			c.spots[i].Frequency == newSpot.Frequency &&
			c.spots[i].Source == newSpot.Source {
			// Found existing spot - update it in place, which resets the timestamp
			c.spots[i] = newSpot
			return true
		}
	}

	// No existing spot found - add as new entry
	c.spots = append(c.spots, newSpot)

	// Remove oldest spots if cache exceeds max size
	if len(c.spots) > c.maxSize {
		c.spots = c.spots[len(c.spots)-c.maxSize:]
	}

	return false
}

// GetAllSpots returns all spots in the cache, sorted by timestamp (newest first).
func (c *Cache) GetAllSpots() []spot.Spot {
	c.RLock()
	defer c.RUnlock()

	// Return a copy to prevent external modification
	spots := append([]spot.Spot{}, c.spots...)

	// Sort by timestamp in descending order (newest first)
	sort.Slice(spots, func(i, j int) bool {
		return spots[i].When.After(spots[j].When)
	})

	return spots
}
