package spot_test

import (
	"fmt"
	"testing"

	"github.com/user00265/dxclustergoapi/internal/spot"
)

func TestBandFromName(t *testing.T) {
	tests := []struct {
		frequencyMHz float64
		expectedBand string
	}{
		// HF Bands
		{1.810, "160m"},
		{1.999, "160m"},
		{3.500, "80m"},
		{3.999, "80m"},
		{7.000, "40m"},
		{7.300, "40m"},
		{10.100, "30m"},
		{10.150, "30m"},
		{14.000, "20m"},
		{14.350, "20m"},
		{18.068, "17m"},
		{18.168, "17m"},
		{21.000, "15m"},
		{21.450, "15m"},
		{24.890, "12m"},
		{24.990, "12m"},
		{28.000, "10m"},
		{29.700, "10m"},

		// WARC / Other HF
		{40.670, "8m"}, // Example, specific ISM band allocation
		{40.680, "8m"},

		// VHF Bands
		{50.000, "6m"},
		{54.000, "6m"},
		{70.000, "4m"},
		{70.500, "4m"},
		{144.000, "2m"},
		{148.000, "2m"},
		{222.000, "1.25m"},
		{225.000, "1.25m"},

		// UHF Bands
		{432.000, "70cm"},
		{439.000, "70cm"},
		{902.000, "33cm"},
		{928.000, "33cm"},

		// SHF Bands
		{1240.000, "23cm"},
		{1295.000, "23cm"},
		{2300.000, "13cm"},
		{2450.000, "13cm"},
		{3400.000, "9cm"},
		{5760.000, "6cm"},
		{10368.000, "3cm"},
		{24192.000, "1.2cm"},
		{47088.000, "6mm"},
		{76000.000, "4mm"},
		{122250.000, "2.5mm"},
		{144000.000, "2mm"},
		{241000.000, "1mm"},

		// Edge cases
		{0.0, "Unknown"},
		{0.5, "Unknown"},
		{2.5, "Unknown"}, // Between bands
		{250.000, "Unknown"},
		{250_000.0, "<1mm"}, // >= 250 GHz
		{260_000.0, "<1mm"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%.3fMHz", tt.frequencyMHz), func(t *testing.T) {
			result := spot.BandFromName(tt.frequencyMHz)
			if result != tt.expectedBand {
				t.Errorf("BandFromName(%.3fMHz) expected %q, got %q", tt.frequencyMHz, tt.expectedBand, result)
			}
		})
	}
}
