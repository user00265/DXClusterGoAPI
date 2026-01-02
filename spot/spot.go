package spot

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/user00265/dxclustergoapi/backend/dxcc"
	"github.com/user00265/dxclustergoapi/backend/lotw"
	"github.com/user00265/dxclustergoapi/utils"
)

// Info represents the enriched DXCC and LoTW information for a callsign.
type Info struct {
	DXCC       *dxcc.DxccInfo     `json:"dxcc_info"`
	LoTW       *lotw.UserActivity `json:"lotw_info"`
	IsLoTWUser bool               `json:"lotw_user"` // Convenience field
}

// Spot represents a single aggregated and enriched ham radio spot.
// This is the canonical struct used throughout the application and for API responses.
type Spot struct {
	Spotter   string    `json:"spotter"`
	Spotted   string    `json:"spotted"`
	Frequency int64     `json:"frequency"` // In Hz (canonical storage unit)
	Message   string    `json:"message"`
	When      time.Time `json:"when"`
	Source    string    `json:"source"` // e.g., "DXCluster", "SOTA", "pota"
	Band      string    `json:"band"`   // e.g., "20m", "40m"
	Mode      string    `json:"mode"`   // e.g., "phone", "cw", "data"
	Submode   string    `json:"submode"` // e.g., "USB", "LSB", "AM"

	SpotterInfo Info `json:"spotter_data"`
	SpottedInfo Info `json:"spotted_data"`

	// Additional data for various activations and references
	AdditionalData struct {
		PotaRef  string `json:"pota_ref,omitempty"`
		PotaMode string `json:"pota_mode,omitempty"`
	} `json:"additional_data,omitempty"`
}

// MarshalJSON customizes the JSON output to match the expected API format
func (s Spot) MarshalJSON() ([]byte, error) {
	// Helper to build the flattened DXCC+LoTW object with optional POTA data
	type FlatInfo struct {
		Cont     string      `json:"cont,omitempty"`
		Entity   string      `json:"entity,omitempty"`
		Flag     string      `json:"flag,omitempty"`
		DXCCID   interface{} `json:"dxcc_id,omitempty"` // string in output
		CQZ      interface{} `json:"cqz,omitempty"`     // string in output - CQ Zone
		ITUZ     interface{} `json:"ituz,omitempty"`    // string in output - ITU Zone
		LoTWUser interface{} `json:"lotw_user"`         // "2" or false
		Lat      interface{} `json:"lat,omitempty"`     // string in output
		Lng      interface{} `json:"lng,omitempty"`     // string in output
		PotaRef  string      `json:"pota_ref,omitempty"`
		PotaMode string      `json:"pota_mode,omitempty"`
	}

	buildFlatInfo := func(info Info, additionalData struct {
		PotaRef  string `json:"pota_ref,omitempty"`
		PotaMode string `json:"pota_mode,omitempty"`
	}) *FlatInfo {
		flat := &FlatInfo{}
		if info.DXCC != nil {
			flat.Cont = info.DXCC.Cont
			flat.Entity = info.DXCC.Entity
			flat.Flag = info.DXCC.Flag
			// Convert numbers to strings to match expected format
			if info.DXCC.DXCCID != 0 {
				flat.DXCCID = fmt.Sprintf("%d", info.DXCC.DXCCID)
			}
			if info.DXCC.CQZ != 0 {
				flat.CQZ = fmt.Sprintf("%d", info.DXCC.CQZ)
			}
			if info.DXCC.ITUZ != 0 {
				flat.ITUZ = fmt.Sprintf("%d", info.DXCC.ITUZ)
			}
			if info.DXCC.Latitude != 0 {
				flat.Lat = fmt.Sprintf("%.1f", info.DXCC.Latitude)
			}
			if info.DXCC.Longitude != 0 {
				flat.Lng = fmt.Sprintf("%.1f", info.DXCC.Longitude)
			}
		}
		// LoTW user: number of days since last upload (matching WaveLog API behavior)
		// This will be an integer like 2, 31, etc., or false if not a LoTW member
		flat.LoTWUser = utils.GetLoTWMemberValue(info.LoTW)
		// Add POTA information
		flat.PotaRef = additionalData.PotaRef
		flat.PotaMode = additionalData.PotaMode
		return flat
	}

	// We don't include spotter info if the spotter is blank or invalid.
	dxccSpotter := buildFlatInfo(s.SpotterInfo, s.AdditionalData)
	if s.SpotterInfo.DXCC == nil && !s.SpotterInfo.IsLoTWUser {
		dxccSpotter = nil
	}
	dxccSpotted := buildFlatInfo(s.SpottedInfo, s.AdditionalData)
	if s.SpottedInfo.DXCC == nil && !s.SpottedInfo.IsLoTWUser {
		dxccSpotted = nil
	}

	// Build the output structure - single variables first, then objects
	// Single variables: spotter, spotted, frequency, band, mode, submode, message, when, source
	// Objects: dxcc_spotter, dxcc_spotted (with optional POTA data)
	// Frequency output is integer kHz (stored internally as Hz)
	// When is formatted with millisecond precision (RFC3339 with 3 decimal places)
	output := struct {
		Spotter     string    `json:"spotter"`
		Spotted     string    `json:"spotted"`
		Frequency   int       `json:"frequency"` // Integer kHz output (e.g., 14272)
		Message     string    `json:"message"`
		When        string    `json:"when"`
		Source      string    `json:"source,omitempty"`
		Band        string    `json:"band"`
		Mode        string    `json:"mode,omitempty"`
		Submode     string    `json:"submode,omitempty"`
		DXCCSpotter *FlatInfo `json:"dxcc_spotter,omitempty"`
		DXCCSpotted *FlatInfo `json:"dxcc_spotted,omitempty"`
	}{
		Spotter:     s.Spotter,
		Spotted:     s.Spotted,
		Frequency:   int(s.Frequency / 1000), // Convert Hz to kHz integer (14250000 Hz -> 14250)
		Message:     s.Message,
		When:        s.When.UTC().Format("2006-01-02T15:04:05.000Z07:00"), // Millisecond precision
		Source:      s.Source,
		Band:        s.Band,
		Mode:        s.Mode,
		Submode:     s.Submode,
		DXCCSpotter: dxccSpotter,
		DXCCSpotted: dxccSpotted,
	}

	return json.Marshal(output)
}
