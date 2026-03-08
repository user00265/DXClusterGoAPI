package utils

import (
	"regexp"
	"strings"
)

// NormalizeCallsign removes common system suffixes and invalid characters
// from a raw callsign string, returning the cleaned canonical form.
// This is used to strip POTA system suffixes (like "-#") and other
// automatic system markers from callsigns before storage/output.
func NormalizeCallsign(raw string) string {
	raw = strings.ToUpper(strings.TrimSpace(raw))

	// Remove the literal "-#" suffix (POTA system marker)
	if idx := strings.Index(raw, "-#"); idx != -1 {
		raw = raw[:idx]
	}
	// Remove any remaining lone '#' characters
	raw = strings.ReplaceAll(raw, "#", "")
	// Trim stray hyphens left by replacements
	raw = strings.Trim(raw, "- ")

	return strings.TrimSpace(raw)
}

// ParsedCallsign represents a tokenized callsign: A/B/C structure.
type ParsedCallsign struct {
	Original string // Original input
	A        string // Prefix (before first /)
	B        string // Core callsign (between / or start/end)
	C        string // Suffix (after second /)
	Raw      string // Cleaned for regex matching (uppercase, trimmed)
}

// reCallsignParts pre-compiled regex for ParseCallsign A/B/C tokenization.
var reCallsignParts = regexp.MustCompile(`^((\d|[A-Z])+\/)?((\d|[A-Z]){3,})(\/(\d|[A-Z])+)?(\/(\d|[A-Z])+)?$`)

// ParseCallsign tokenizes a raw callsign into A/B/C structure (prefix/callsign/suffix).
// Does NOT apply DXCC special rules; that is done by LookupDXCC.
func ParseCallsign(raw string) ParsedCallsign {
	original := strings.ToUpper(strings.TrimSpace(raw))

	result := ParsedCallsign{
		Original: original,
	}

	// Delegate cleanup to NormalizeCallsign to avoid duplication
	cleaned := NormalizeCallsign(raw)
	result.Raw = cleaned

	matches := reCallsignParts.FindStringSubmatch(cleaned)

	if matches == nil {
		// No slashes; treat entire string as B (callsign)
		result.B = cleaned
		return result
	}

	// matches[1] = A with "/" (or empty)
	// matches[3] = B (main callsign)
	// matches[5] = C with "/" (or empty)
	result.A = strings.TrimSuffix(matches[1], "/")
	result.B = matches[3]
	result.C = strings.TrimPrefix(matches[5], "/")

	return result
}
