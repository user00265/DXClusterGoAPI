package cluster

import (
	"testing"
)

// TestTryLenientParseDX tests the fallback parser for edge cases and invalid formats.
func TestTryLenientParseDX(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		expectOk   bool
		expSpotter string
		expSpotted string
		expFreq    string
		expMsg     string
	}{
		{
			name:       "Valid standard format",
			input:      "DX de K1ABC: 14250.0 G4XYZ Hello world 1234Z",
			expectOk:   true,
			expSpotter: "K1ABC",
			expSpotted: "G4XYZ",
			expFreq:    "14250.0",
			expMsg:     "Hello world",
		},
		{
			name:       "Extra spaces between fields",
			input:      "DX de OK2IT:      3796.0  W5BN       test message  1522Z",
			expectOk:   true,
			expSpotter: "OK2IT",
			expSpotted: "W5BN",
			expFreq:    "3796.0",
			expMsg:     "test message",
		},
		{
			name:     "Invalid: spotted callsign is single char (I)",
			input:    "DX de OK2IT:      3796.0  I            am cheating agn with sdr RX!   1522Z",
			expectOk: false,
		},
		{
			name:     "Invalid: spotter callsign is too short",
			input:    "DX de AB:      3796.0  W5BN       test  1522Z",
			expectOk: false,
		},
		{
			name:     "Invalid: spotted callsign is too short",
			input:    "DX de OK2IT:      3796.0  XY       test  1522Z",
			expectOk: false,
		},
		{
			name:       "Valid: with locator suffix",
			input:      "DX de VE3/K1ABC: 7100.5 G4XYZ SOTA spot 1800Z",
			expectOk:   true,
			expSpotter: "VE3/K1ABC",
			expSpotted: "G4XYZ",
			expFreq:    "7100.5",
			expMsg:     "SOTA spot",
		},
		{
			name:     "Invalid: missing frequency",
			input:    "DX de K1ABC:  G4XYZ test message 1234Z",
			expectOk: false,
		},
		{
			name:     "Invalid: missing timestamp",
			input:    "DX de K1ABC: 14250.0 G4XYZ test message",
			expectOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spotter, spotted, freq, msg, ok := tryLenientParseDX(tt.input)

			if ok != tt.expectOk {
				t.Errorf("tryLenientParseDX(%q) ok = %v, want %v", tt.input, ok, tt.expectOk)
			}

			if !tt.expectOk {
				// For invalid cases, we don't check the returned values
				return
			}

			if spotter != tt.expSpotter {
				t.Errorf("spotter = %q, want %q", spotter, tt.expSpotter)
			}
			if spotted != tt.expSpotted {
				t.Errorf("spotted = %q, want %q", spotted, tt.expSpotted)
			}
			if freq != tt.expFreq {
				t.Errorf("freq = %q, want %q", freq, tt.expFreq)
			}
			if msg != tt.expMsg {
				t.Errorf("msg = %q, want %q", msg, tt.expMsg)
			}
		})
	}
}
