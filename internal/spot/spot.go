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

// BandFromName converts a frequency to a ham radio band string.
// Accepts frequency in Hz, kHz, or MHz and automatically detects the unit.
// This function mirrors the qrg2band from the Node.js original.
func BandFromName(frequency float64) string {
	// Auto-detect units and normalize to MHz for band detection
	var frequencyMHz float64

	if frequency >= 1000000 { // >= 1MHz, assume Hz
		frequencyMHz = frequency / 1000000.0
	} else if frequency >= 1000 { // >= 1kHz, assume kHz
		frequencyMHz = frequency / 1000.0
	} else { // < 1kHz, assume already MHz
		frequencyMHz = frequency
	}

	switch {
	case frequencyMHz >= 1.8 && frequencyMHz <= 2.0:
		return "160m"
	case frequencyMHz >= 3.5 && frequencyMHz <= 4.0:
		return "80m"
	case frequencyMHz >= 7.0 && frequencyMHz <= 7.3:
		return "40m"
	case frequencyMHz >= 10.1 && frequencyMHz <= 10.15:
		return "30m"
	case frequencyMHz >= 14.0 && frequencyMHz <= 14.35:
		return "20m"
	case frequencyMHz >= 18.068 && frequencyMHz <= 18.168:
		return "17m"
	case frequencyMHz >= 21.0 && frequencyMHz <= 21.45:
		return "15m"
	case frequencyMHz >= 24.89 && frequencyMHz <= 24.99:
		return "12m"
	case frequencyMHz >= 28.0 && frequencyMHz <= 29.7:
		return "10m"
	case frequencyMHz >= 50.0 && frequencyMHz <= 54.0:
		return "6m"
	case frequencyMHz >= 144.0 && frequencyMHz <= 148.0:
		return "2m"
	case frequencyMHz >= 220.0 && frequencyMHz <= 225.0:
		return "1.25m"
	case frequencyMHz >= 430.0 && frequencyMHz <= 450.0:
		return "70cm"
	case frequencyMHz >= 902.0 && frequencyMHz <= 928.0:
		return "33cm"
	case frequencyMHz >= 1240.0 && frequencyMHz <= 1300.0:
		return "23cm"
	case frequencyMHz >= 2300.0 && frequencyMHz <= 2450.0:
		return "13cm"
	case frequencyMHz >= 3300.0 && frequencyMHz <= 3500.0:
		return "9cm"
	case frequencyMHz >= 5650.0 && frequencyMHz <= 5925.0:
		return "6cm"
	case frequencyMHz >= 10000.0 && frequencyMHz <= 10500.0:
		return "3cm"
	case frequencyMHz >= 24000.0 && frequencyMHz <= 24250.0:
		return "1.2cm"
	case frequencyMHz >= 47000.0 && frequencyMHz <= 47200.0:
		return "6mm"
	case frequencyMHz >= 75500.0 && frequencyMHz <= 81500.0:
		return "4mm"
	case frequencyMHz >= 119980.0 && frequencyMHz <= 120020.0:
		return "2.5mm"
	case frequencyMHz >= 142000.0 && frequencyMHz <= 149000.0:
		return "2mm"
	case frequencyMHz >= 241000.0 && frequencyMHz <= 250000.0:
		return "1mm"
	case frequencyMHz >= 250000.0:
		return "<1mm"
	default:
		return "Unknown"
	}
}
