package spot

import (
	"time"

	"github.com/user00265/dxclustergoapi/internal/dxcc"
	"github.com/user00265/dxclustergoapi/internal/lotw"
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
	Frequency float64   `json:"frequency"` // In MHz
	Message   string    `json:"message"`
	When      time.Time `json:"when"`
	Source    string    `json:"source"` // e.g., "DXCluster", "SOTA", "pota"
	Band      string    `json:"band"`   // e.g., "20m", "40m"

	SpotterInfo Info `json:"spotter_data"`
	SpottedInfo Info `json:"spotted_data"`

	// Additional data for POTA, if applicable
	AdditionalData struct {
		PotaRef  string `json:"pota_ref,omitempty"`
		PotaMode string `json:"pota_mode,omitempty"`
	} `json:"additional_data,omitempty"`
}

// BandFromName converts a frequency (in MHz) to a ham radio band string.
// This function mirrors the qrg2band from the Node.js original.
func BandFromName(frequencyMHz float64) string {
	frequencyHz := frequencyMHz * 1_000_000 // Convert MHz to Hz

	switch {
	case frequencyHz >= 1_000_000 && frequencyHz < 2_000_000:
		return "160m"
	case frequencyHz >= 3_000_000 && frequencyHz < 4_000_000:
		return "80m"
	case frequencyHz >= 6_000_000 && frequencyHz < 8_000_000:
		return "40m"
	case frequencyHz >= 9_000_000 && frequencyHz < 11_000_000:
		return "30m"
	case frequencyHz >= 13_000_000 && frequencyHz < 15_000_000:
		return "20m"
	case frequencyHz >= 17_000_000 && frequencyHz < 19_000_000:
		return "17m"
	case frequencyHz >= 20_000_000 && frequencyHz < 22_000_000:
		return "15m"
	case frequencyHz >= 23_000_000 && frequencyHz < 25_000_000:
		return "12m"
	case frequencyHz >= 27_000_000 && frequencyHz < 30_000_000:
		return "10m"
	case frequencyHz >= 40_660_000 && frequencyHz < 40_690_000:
		return "8m" // Often for 8m band experimentation
	case frequencyHz >= 49_000_000 && frequencyHz <= 54_000_000:
		return "6m"
	case frequencyHz >= 69_000_000 && frequencyHz < 71_000_000:
		return "4m"
	case frequencyHz >= 140_000_000 && frequencyHz < 150_000_000:
		return "2m"
	case frequencyHz >= 218_000_000 && frequencyHz < 226_000_000:
		return "1.25m"
	case frequencyHz >= 430_000_000 && frequencyHz < 440_000_000:
		return "70cm"
	case frequencyHz >= 900_000_000 && frequencyHz < 930_000_000:
		return "33cm"
	case frequencyHz >= 1_200_000_000 && frequencyHz < 1_300_000_000:
		return "23cm"
	case frequencyHz >= 2_200_000_000 && frequencyHz < 2_600_000_000:
		return "13cm"
	case frequencyHz >= 3_000_000_000 && frequencyHz < 4_000_000_000:
		return "9cm"
	case frequencyHz >= 5_000_000_000 && frequencyHz < 6_000_000_000:
		return "6cm"
	case frequencyHz >= 9_000_000_000 && frequencyHz < 11_000_000_000:
		return "3cm"
	case frequencyHz >= 23_000_000_000 && frequencyHz < 25_000_000_000:
		return "1.2cm"
	case frequencyHz >= 46_000_000_000 && frequencyHz < 55_000_000_000:
		return "6mm"
	case frequencyHz >= 75_000_000_000 && frequencyHz < 82_000_000_000:
		return "4mm"
	case frequencyHz >= 120_000_000_000 && frequencyHz < 125_000_000_000:
		return "2.5mm"
	case frequencyHz >= 133_000_000_000 && frequencyHz < 150_000_000_000:
		return "2mm"
	case frequencyHz >= 240_000_000_000 && frequencyHz < 250_000_000_000:
		return "1mm"
	case frequencyHz >= 250_000_000_000:
		return "<1mm"
	default:
		return "Unknown"
	}
}
