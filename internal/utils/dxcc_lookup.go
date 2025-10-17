package utils

import (
	"regexp"
	"strings"

	"github.com/user00265/dxclustergoapi/internal/dxcc"
)

// LookupDXCC performs DXCC entity lookup for a callsign.
func LookupDXCC(call string, client *dxcc.Client) (*dxcc.DxccInfo, error) {
	call = strings.ToUpper(strings.TrimSpace(call))

	// Check exceptions first
	if entity, found := client.GetException(call); found {
		return entity, nil
	}

	// Apply special-case canonicalization rules
	call = applySpecialRules(call)

	// Parse A/B/C structure to check for slashes
	parsed := ParseCallsign(call)

	csadditions := regexp.MustCompile(`^P$|^R$|^A$|^B$|^M$`)

	// If we have a slash (A or C present), handle it
	if parsed.A != "" || parsed.C != "" {
		if csadditions.MatchString(parsed.C) {
			// Portable/temporary suffix: use prefix if available, else callsign
			if parsed.A != "" {
				call = parsed.A
			} else {
				call = parsed.B
			}
		} else if parsed.C != "" {
			// Non-portable suffix: use WPX-style prefix computation
			wpxPrefix := wpx(call, nil)
			if wpxPrefix == "" {
				// wpx() returned empty → not found
				return nil, nil
			}
			// wpx() returned a prefix: append "AA" and use for lookup
			call = wpxPrefix + "AA"
		}
		// If A is set but C is empty, we still have the call as-is (no special handling needed)
	}

	// Progressive prefix matching (longest to shortest)
	for i := len(call); i > 0; i-- {
		prefix := call[:i]
		if entity, found := client.GetPrefix(prefix); found {
			return entity, nil
		}
	}

	// Not found
	return nil, nil
}

// applySpecialRules applies special-case canonicalization rules.
func applySpecialRules(call string) string {
	// KG4 special cases (Guantanamo Bay)
	if regexp.MustCompile(`^(KG4)[A-Z0-9]{3}`).MatchString(call) {
		return "K"
	}
	if regexp.MustCompile(`^(KG4)[A-Z0-9]{2}`).MatchString(call) {
		return "KG4"
	}
	if regexp.MustCompile(`^(KG4)[A-Z0-9]{1}`).MatchString(call) {
		return "K"
	}

	// OH/ or /OH[1-9]? (non-Åland, so OH = Finland)
	if regexp.MustCompile(`(^OH\/)|(\/OH[1-9]?$)`).MatchString(call) {
		return "OH"
	}

	// CX/ or /CX[1-9]? (non-Antarctica, so CX = Uruguay)
	if regexp.MustCompile(`(^CX\/)|(\/CX[1-9]?$)`).MatchString(call) {
		return "CX"
	}

	// 3D2R or 3D2.+/R (Rotuma)
	if regexp.MustCompile(`(^3D2R)|(^3D2.+\/R)`).MatchString(call) {
		return "3D2/R"
	}

	// 3D2C (Conway Reef)
	if regexp.MustCompile(`^3D2C`).MatchString(call) {
		return "3D2/C"
	}

	// LZ/ or /LZ[1-9]? (LZ0 by DXCC but this is VP8h, so LZ)
	if regexp.MustCompile(`(^LZ\/)|(\/LZ[1-9]?$)`).MatchString(call) {
		return "LZ"
	}

	// No special rule matched
	return call
}

// wpx computes a WPX-style prefix for a callsign.
// Returns empty string if the callsign is invalid (all-digit B part, maritime suffix, etc.).
func wpx(testcall string, i *int) string {
	lidadditions := regexp.MustCompile(`^QRP$|^LGT$`)
	csadditions := regexp.MustCompile(`^X$|^D$|^T$|^P$|^R$|^B$|^A$|^M$`)
	noneadditions := regexp.MustCompile(`^MM$|^AM$`)

	re := regexp.MustCompile(`^((\d|[A-Z])+\/)?((\d|[A-Z]){3,})(\/(\d|[A-Z])+)?(\/(\d|[A-Z])+)?$`)
	matches := re.FindStringSubmatch(testcall)

	if matches == nil {
		return ""
	}

	a := strings.TrimSuffix(matches[1], "/") // A (prefix, without /)
	b := matches[3]                          // B (core callsign)
	c := strings.TrimPrefix(matches[5], "/") // C (suffix, without /)

	// Handle lid additions and swapping
	if c == "" && a != "" && b != "" {
		if lidadditions.MatchString(b) {
			// Swap: b becomes a, a becomes empty
			b = a
			a = ""
		} else if regexp.MustCompile(`\d[A-Z]+$`).MatchString(a) && regexp.MustCompile(`\d$`).MatchString(b) {
			// Swap a and b
			a, b = b, a
		}
	}

	// If B is all digits, invalid
	if regexp.MustCompile(`^[0-9]+$`).MatchString(b) {
		return ""
	}

	var prefix string

	// Determine prefix per WPX-like rules
	if a == "" && c == "" {
		// No prefix, no suffix
		if regexp.MustCompile(`\d`).MatchString(b) {
			// B contains digit: extract up to last digit
			m := regexp.MustCompile(`(.+\d)[A-Z]*`).FindStringSubmatch(b)
			if len(m) > 1 {
				prefix = m[1]
			} else {
				prefix = b
			}
		} else {
			// B is all letters: take first 2 + "0"
			if len(b) >= 2 {
				prefix = b[:2] + "0"
			} else {
				prefix = b + "0"
			}
		}
	} else if a == "" && c != "" {
		// No prefix, has suffix C
		if regexp.MustCompile(`^(\d)`).MatchString(c) {
			// C starts with digit
			m := regexp.MustCompile(`(.+\d)[A-Z]*`).FindStringSubmatch(b)
			if len(m) > 1 {
				bPart := m[1]
				if regexp.MustCompile(`^([A-Z]\d)\d$`).MatchString(bPart) {
					prefix = bPart + c
				} else {
					m2 := regexp.MustCompile(`(.*[A-Z])\d+`).FindStringSubmatch(bPart)
					if len(m2) > 1 {
						prefix = m2[1] + c
					} else {
						prefix = bPart + c
					}
				}
			} else {
				prefix = b + c
			}
		} else if csadditions.MatchString(c) {
			// C is portable/temp suffix
			m := regexp.MustCompile(`(.+\d)[A-Z]*`).FindStringSubmatch(b)
			if len(m) > 1 {
				prefix = m[1]
			} else {
				prefix = b
			}
		} else if noneadditions.MatchString(c) {
			// Maritime (MM, AM)
			return ""
		} else {
			// C is a normal suffix: use C, or C+"0" if no digit
			if regexp.MustCompile(`\d$`).MatchString(c) {
				prefix = c
			} else {
				prefix = c + "0"
			}
		}
	} else if a != "" && noneadditions.MatchString(c) {
		// Prefix present and maritime suffix
		return ""
	} else if a != "" {
		// Has prefix A; use it, optionally add "0" if no digit
		if regexp.MustCompile(`\d$`).MatchString(a) {
			prefix = a
		} else {
			prefix = a + "0"
		}
	}

	// Final cleanup: if prefix matches pattern and i is nil, simplify
	if i == nil && regexp.MustCompile(`(\w+\d)[A-Z]+\d`).MatchString(prefix) {
		m := regexp.MustCompile(`(\w+\d)[A-Z]+\d`).FindStringSubmatch(prefix)
		if len(m) > 1 {
			prefix = m[1]
		}
	}

	return prefix
}
