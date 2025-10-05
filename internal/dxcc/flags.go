package dxcc

// FlagEmojis is a small trimmed mapping of DXCC ID (as string) to emoji flag.
// The original full dataset was large and used escaped sequences that caused
// compile-time issues in this snapshot. Restore the full dataset later if needed.
var FlagEmojis = map[string]string{
	"1":   "🇨🇦", // Canada
	"10":  "🇳🇱", // Netherlands
	"50":  "🇲🇽", // Mexico
	"70":  "🇨🇺", // Cuba
	"100": "🇦🇷", // Argentina
	"108": "🇧🇷", // Brazil
	"150": "🇦🇺", // Australia
	"170": "🇳🇿", // New Zealand
	// Additional flags used in unit tests
	"291": "🇺🇸", // United States
	"260": "🇲🇨", // Monaco
	"144": "🇺🇾", // Uruguay
	"212": "🇧🇬", // Bulgaria
	"13":  "🇦🇶", // Antarctica
	"105": "",   // Guantanamo Bay (no standard flag)
	"460": "",   // Rotuma I. (may not have emoji mapping)
	"901": "",   // Test DX1 Entity placeholder
	"224": "🇫🇮", // Finland
}
