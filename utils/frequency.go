package utils

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// FrequencyUnit represents the unit of a frequency value
type FrequencyUnit int

const (
	HzUnit FrequencyUnit = iota
	KHzUnit
	MHzUnit
	GHzUnit
)

// ParseFrequency parses a frequency string or number and returns it in Hz (integer).
// Automatically detects the input unit (Hz, kHz, MHz, GHz) based on magnitude.
//
// Input examples:
//   - "14250" -> auto-detected as kHz -> 14250000 Hz
//   - "14.250" -> auto-detected as MHz -> 14250000 Hz
//   - "0.014250" -> auto-detected as GHz -> 14250000 Hz
//   - "14250000" -> auto-detected as Hz -> 14250000 Hz
//   - 14250 (int) -> auto-detected as kHz -> 14250000 Hz
//   - 14.250 (float) -> auto-detected as MHz -> 14250000 Hz
//
// Returns: frequency in Hz (int64), or error if parsing fails
func ParseFrequency(input interface{}) (int64, error) {
	var floatVal float64
	var err error

	// Convert input to float64
	switch v := input.(type) {
	case string:
		// Handle string by replacing common decimal separators
		cleanStr := strings.ReplaceAll(v, ",", ".")
		floatVal, err = strconv.ParseFloat(cleanStr, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid frequency string '%v': %w", input, err)
		}
	case float64:
		floatVal = v
	case float32:
		floatVal = float64(v)
	case int:
		floatVal = float64(v)
	case int64:
		floatVal = float64(v)
	default:
		return 0, fmt.Errorf("unsupported frequency type: %T", input)
	}

	if floatVal <= 0 {
		return 0, fmt.Errorf("frequency must be positive: %v", input)
	}

	// Auto-detect unit based on magnitude and convert to Hz
	var hz float64
	switch {
	case floatVal >= 1_000_000:
		// Likely in Hz already (e.g., 14,250,000 Hz)
		hz = floatVal
	case floatVal >= 1000:
		// Likely in kHz (e.g., 14250 kHz)
		hz = floatVal * 1000
	case floatVal >= 1:
		// Likely in MHz (e.g., 14.250 MHz)
		hz = floatVal * 1_000_000
	default:
		// Likely in GHz (e.g., 0.01425 GHz)
		hz = floatVal * 1_000_000_000
	}

	return int64(math.Round(hz)), nil
}

// FrequencyTo converts a frequency from Hz to a target unit.
// Returns the frequency value in the target unit with appropriate precision.
//
// For Hz: always returns integer (no decimals)
// For kHz, MHz, GHz: returns float64 with proper decimal representation
func FrequencyTo(hz int64, unit FrequencyUnit) interface{} {
	switch unit {
	case HzUnit:
		return hz
	case KHzUnit:
		return float64(hz) / 1000.0
	case MHzUnit:
		return float64(hz) / 1_000_000.0
	case GHzUnit:
		return float64(hz) / 1_000_000_000.0
	default:
		return hz // default to Hz
	}
}

// FormatFrequency formats a frequency in Hz to a human-readable string.
// Automatically chooses the best unit (GHz, MHz, kHz, or Hz) for readability.
//
// Output format:
//   - Hz: "1500000" (no decimals)
//   - kHz: "1500.000 kHz" (3 decimals minimum)
//   - MHz: "1.500 MHz" (3 decimals minimum)
//   - GHz: "1.500 GHz" (3 decimals minimum)
//
// Example outputs:
//   - 14250000 Hz -> "14.250 MHz"
//   - 50125000 Hz -> "50.125 MHz"
//   - 2400000000 Hz -> "2.400 GHz"
//   - 1200000 Hz -> "1200.000 kHz"
func FormatFrequency(hz int64) string {
	const eps = 1e-9

	// Convert to MHz first for decision-making
	mhz := float64(hz) / 1_000_000.0

	// Choose unit based on magnitude
	switch {
	case mhz >= 1000:
		// Use GHz for frequencies >= 1 GHz
		ghz := mhz / 1000.0
		return fmt.Sprintf("%.3f GHz", ghz)
	case mhz >= 1:
		// Use MHz for frequencies >= 1 MHz
		return fmt.Sprintf("%.3f MHz", mhz)
	case mhz >= 0.001:
		// Use kHz for frequencies >= 1 kHz
		khz := mhz * 1000.0
		return fmt.Sprintf("%.3f kHz", khz)
	default:
		// Use Hz for frequencies < 1 kHz
		return fmt.Sprintf("%d Hz", hz)
	}
}

// FormatFrequencyAs formats a frequency in Hz to a specific unit with 3 decimal places.
// For Hz, returns an integer with no decimals.
//
// Examples:
//   - FormatFrequencyAs(14250000, MHzUnit) -> "14.250 MHz"
//   - FormatFrequencyAs(14250000, KHzUnit) -> "14250.000 kHz"
//   - FormatFrequencyAs(1200000000, GHzUnit) -> "1.200 GHz"
//   - FormatFrequencyAs(1500000, HzUnit) -> "1500000 Hz"
func FormatFrequencyAs(hz int64, unit FrequencyUnit) string {
	switch unit {
	case HzUnit:
		return fmt.Sprintf("%d Hz", hz)
	case KHzUnit:
		khz := float64(hz) / 1000.0
		return fmt.Sprintf("%.3f kHz", khz)
	case MHzUnit:
		mhz := float64(hz) / 1_000_000.0
		return fmt.Sprintf("%.3f MHz", mhz)
	case GHzUnit:
		ghz := float64(hz) / 1_000_000_000.0
		return fmt.Sprintf("%.3f GHz", ghz)
	default:
		return fmt.Sprintf("%d Hz", hz)
	}
}

// NormalizeToHz takes any frequency representation and returns it as Hz (int64).
// This is a convenience wrapper around ParseFrequency for code that always expects Hz output.
//
// Example:
//   - NormalizeToHz("14250") -> 14250000
//   - NormalizeToHz(14.250) -> 14250000
func NormalizeToHz(input interface{}) (int64, error) {
	return ParseFrequency(input)
}

// FrequencyDeviation checks if two frequencies (in Hz) are within a certain tolerance.
// Useful for detecting duplicate spots with slight frequency drift.
//
// Parameters:
//   - freq1, freq2: frequencies in Hz (int64)
//   - toleranceHz: tolerance in Hz (e.g., 3000 for ±3 kHz)
//
// Returns: true if |freq1 - freq2| <= toleranceHz
func FrequencyDeviation(freq1, freq2 int64, toleranceHz int64) bool {
	diff := int64(math.Abs(float64(freq1 - freq2)))
	return diff <= toleranceHz
}
