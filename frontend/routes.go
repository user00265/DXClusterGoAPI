package frontend

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/user00265/dxclustergoapi/backend/dxcc"
	"github.com/user00265/dxclustergoapi/backend/lotw"
	"github.com/user00265/dxclustergoapi/spot"
	"github.com/user00265/dxclustergoapi/utils"
)

// SpotCache defines the interface for accessing cached spots
type SpotCache interface {
	GetAllSpots() []spot.Spot
	AddSpot(newSpot spot.Spot) bool
}

// SetupRoutes configures all API endpoints
func SetupRoutes(r *gin.RouterGroup, cache SpotCache, dxccClient *dxcc.Client, lotwClient *lotw.Client) {
	// GET /spot/:qrg - Retrieve the latest spot for a given frequency (QRG).
	// Supports: Hz (14250000), kHz (14250), MHz (14.250)
	r.GET("/spot/:qrg", func(c *gin.Context) {
		qrgParam := c.Param("qrg")

		// Parse frequency input to Hz using utils
		qrgHz, err := utils.ParseFrequency(qrgParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid frequency format: %v", err)})
			return
		}

		// Use 3 kHz tolerance for frequency matching (in Hz, so 3000 Hz)
		toleranceHz := int64(3000)

		spots := cache.GetAllSpots()
		var latestSpot *spot.Spot
		var youngestTime time.Time

		for i := range spots {
			// Compare frequencies with tolerance (in Hz)
			if utils.FrequencyDeviation(spots[i].Frequency, qrgHz, toleranceHz) {
				if latestSpot == nil || spots[i].When.After(youngestTime) {
					latestSpot = &spots[i]
					youngestTime = spots[i].When
				}
			}
		}

		if latestSpot != nil {
			c.JSON(http.StatusOK, latestSpot)
		} else {
			c.JSON(http.StatusNotFound, gin.H{"error": "No spot found for this frequency"})
		}
	})

	// GET /spots - Retrieve all cached spots.
	r.GET("/spots", func(c *gin.Context) {
		spots := cache.GetAllSpots()
		c.JSON(http.StatusOK, spots)
	})

	// GET /spots/:band - Retrieve all cached spots for a given band.
	r.GET("/spots/:band", func(c *gin.Context) {
		bandParam := strings.ToLower(c.Param("band"))

		// Normalize band parameter - accept both "20m" and "20"
		normalizedBand := bandParam
		if bandInt, err := strconv.Atoi(bandParam); err == nil {
			// If it's a number, add "m" suffix for meters
			normalizedBand = fmt.Sprintf("%dm", bandInt)
		}
		// For centimeter bands like "70cm", "33cm", user must specify full name

		filteredSpots := make([]spot.Spot, 0)
		for _, s := range cache.GetAllSpots() {
			if strings.ToLower(s.Band) == normalizedBand {
				filteredSpots = append(filteredSpots, s)
			}
		}
		c.JSON(http.StatusOK, filteredSpots)
	})

	// GET /spots/source/:source - Retrieve all cached spots from a given source.
	r.GET("/spots/source/:source", func(c *gin.Context) {
		sourceParam := strings.ToLower(c.Param("source"))
		filteredSpots := make([]spot.Spot, 0)
		for _, s := range cache.GetAllSpots() {
			if strings.ToLower(s.Source) == sourceParam {
				filteredSpots = append(filteredSpots, s)
			}
		}
		c.JSON(http.StatusOK, filteredSpots)
	})

	// GET /spots/callsign/:callsign - Retrieve all cached spots involving a given callsign (spotter or spotted).
	r.GET("/spots/callsign/:callsign", func(c *gin.Context) {
		callsignParam := strings.ToUpper(c.Param("callsign"))
		filteredSpots := make([]spot.Spot, 0)
		for _, s := range cache.GetAllSpots() {
			if strings.ToUpper(s.Spotter) == callsignParam || strings.ToUpper(s.Spotted) == callsignParam {
				filteredSpots = append(filteredSpots, s)
			}
		}
		// Sort by 'When' descending (latest first)
		sort.Slice(filteredSpots, func(i, j int) bool {
			return filteredSpots[i].When.After(filteredSpots[j].When)
		})
		c.JSON(http.StatusOK, filteredSpots)
	})

	// GET /spotter/:callsign - Retrieve all cached spots where the given callsign is the spotter.
	r.GET("/spotter/:callsign", func(c *gin.Context) {
		callsignParam := strings.ToUpper(c.Param("callsign"))
		filteredSpots := make([]spot.Spot, 0)
		for _, s := range cache.GetAllSpots() {
			if strings.ToUpper(s.Spotter) == callsignParam {
				filteredSpots = append(filteredSpots, s)
			}
		}
		sort.Slice(filteredSpots, func(i, j int) bool {
			return filteredSpots[i].When.After(filteredSpots[j].When)
		})
		c.JSON(http.StatusOK, filteredSpots)
	})

	// GET /spotted/:callsign - Retrieve all cached spots where the given callsign is the spotted station.
	r.GET("/spotted/:callsign", func(c *gin.Context) {
		callsignParam := strings.ToUpper(c.Param("callsign"))
		filteredSpots := make([]spot.Spot, 0)
		for _, s := range cache.GetAllSpots() {
			if strings.ToUpper(s.Spotted) == callsignParam {
				filteredSpots = append(filteredSpots, s)
			}
		}
		sort.Slice(filteredSpots, func(i, j int) bool {
			return filteredSpots[i].When.After(filteredSpots[j].When)
		})
		c.JSON(http.StatusOK, filteredSpots)
	})

	// GET /spots/dxcc/:dxcc - Retrieve all cached spots for a given DXCC entity ID or continent.
	r.GET("/spots/dxcc/:dxcc", func(c *gin.Context) {
		dxccParam := c.Param("dxcc")

		// Try to parse as DXCC ID number first
		dxccID, err := strconv.Atoi(dxccParam)
		var isNumeric bool = (err == nil)

		// If not numeric, treat as continent abbreviation (convert to uppercase)
		continent := strings.ToUpper(dxccParam)

		filteredSpots := make([]spot.Spot, 0)
		for _, s := range cache.GetAllSpots() {
			var matches bool = false

			if isNumeric {
				// Match by DXCC ID
				matches = (s.SpottedInfo.DXCC != nil && s.SpottedInfo.DXCC.DXCCID == dxccID) ||
					(s.SpotterInfo.DXCC != nil && s.SpotterInfo.DXCC.DXCCID == dxccID)
			} else {
				// Match by continent abbreviation
				matches = (s.SpottedInfo.DXCC != nil && s.SpottedInfo.DXCC.Cont == continent) ||
					(s.SpotterInfo.DXCC != nil && s.SpotterInfo.DXCC.Cont == continent)
			}

			if matches {
				filteredSpots = append(filteredSpots, s)
			}
		}
		sort.Slice(filteredSpots, func(i, j int) bool {
			return filteredSpots[i].When.After(filteredSpots[j].When)
		})
		c.JSON(http.StatusOK, filteredSpots)
	})

	// GET /stats - Retrieve statistics about the cached spots.
	r.GET("/stats", func(c *gin.Context) {
		spots := cache.GetAllSpots()
		stats := gin.H{
			"entries":  len(spots),
			"cluster":  0,
			"pota":     0,
			"freshest": nil, // Will be ISO string or null
			"oldest":   nil, // Will be ISO string or null
		}

		// Add DXCC and LoTW last update times
		if dxccLastUpdate, err := dxccClient.GetLastDownloadTime(context.Background()); err == nil && !dxccLastUpdate.IsZero() {
			stats["dxcc_last_updated"] = dxccLastUpdate.Format(time.RFC3339)
		} else {
			stats["dxcc_last_updated"] = nil
		}

		if lotwLastUpdate, err := lotwClient.GetLastDownloadTime(context.Background()); err == nil && !lotwLastUpdate.IsZero() {
			stats["lotw_last_updated"] = lotwLastUpdate.Format(time.RFC3339)
		} else {
			stats["lotw_last_updated"] = nil
		}

		if len(spots) > 0 {
			youngest := time.Time{}                              // Zero time
			oldest := time.Now().Add(100 * 365 * 24 * time.Hour) // Far future

			for i := range spots {
				// Treat any non-pota source as a cluster-derived spot for stats
				if strings.ToLower(spots[i].Source) == "pota" {
					stats["pota"] = stats["pota"].(int) + 1
				} else {
					stats["cluster"] = stats["cluster"].(int) + 1
				}

				if spots[i].When.After(youngest) {
					youngest = spots[i].When
				}
				if spots[i].When.Before(oldest) {
					oldest = spots[i].When
				}
			}
			stats["freshest"] = youngest.Format(time.RFC3339)
			stats["oldest"] = oldest.Format(time.RFC3339)
		}
		c.JSON(http.StatusOK, stats)
	})

	// Test-only debug endpoint: reports per-source counts.
	// This route is registered only when DX_API_TEST_DEBUG=1 is set so it
	// remains unavailable in production by default.
	if v := os.Getenv("DX_API_TEST_DEBUG"); v == "1" || strings.ToLower(v) == "true" {
		r.GET("/debug", func(c *gin.Context) {
			counts := make(map[string]int)
			for _, s := range cache.GetAllSpots() {
				src := strings.ToLower(s.Source)
				counts[src]++
			}
			c.JSON(http.StatusOK, gin.H{"per_source": counts})
		})
	}
}
