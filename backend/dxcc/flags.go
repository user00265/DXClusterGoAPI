package dxcc

// FlagEmojis is a comprehensive mapping of DXCC ID (as string) to emoji flag.
// This covers the most common amateur radio countries and entities.
var FlagEmojis = map[string]string{
	// North America
	"1":   "🇨🇦", // Canada
	"291": "🇺🇸", // United States
	"50":  "🇲🇽", // Mexico
	"70":  "🇨🇺", // Cuba
	"105": "🇺🇸", // Guantanamo Bay (US territory)

	// South America
	"100": "🇦🇷", // Argentina
	"108": "🇧🇷", // Brazil
	"144": "🇺🇾", // Uruguay
	"112": "🇨🇱", // Chile
	"116": "🇨🇴", // Colombia
	"136": "🇵🇪", // Peru
	"140": "🇻🇪", // Venezuela
	"120": "🇪🇨", // Ecuador
	"124": "🇬🇾", // Guyana
	"132": "🇵🇾", // Paraguay
	"129": "🇬🇫", // French Guiana

	// Europe
	"10":  "🇳🇱", // Netherlands
	"14":  "🇦🇹", // Austria
	"15":  "🇧🇪", // Belgium
	"503": "🇨🇿", // Czech Republic
	"160": "🇩🇰", // Denmark
	"224": "🇫🇮", // Finland
	"227": "🇫🇷", // France
	"230": "🇩🇪", // Germany
	"236": "🇬🇷", // Greece
	"239": "🇭🇺", // Hungary
	"251": "🇮🇸", // Iceland
	"245": "🇮🇪", // Ireland
	"248": "🇮🇹", // Italy
	"260": "🇲🇨", // Monaco
	"266": "🇳🇴", // Norway
	"269": "🇵🇱", // Poland
	"272": "🇵🇹", // Portugal
	"54":  "🇷🇺", // Russia
	"281": "🇪🇸", // Spain
	"284": "🇸🇪", // Sweden
	"287": "🇨🇭", // Switzerland
	"288": "🇺🇦", // Ukraine
	"223": "🇬🇧", // United Kingdom (England)
	"212": "🇧🇬", // Bulgaria
	"275": "🇷🇴", // Romania

	// Asia
	"339": "🇯🇵", // Japan
	"318": "🇨🇳", // China
	"137": "🇰🇷", // Korea, Republic of
	"324": "🇮🇳", // India
	"387": "🇹🇭", // Thailand
	"293": "🇻🇳", // Vietnam
	"327": "🇮🇩", // Indonesia
	"375": "🇵🇭", // Philippines
	"381": "🇸🇬", // Singapore
	"333": "🇲🇾", // Malaysia
	"390": "🇹🇼", // Taiwan
	"321": "🇭🇰", // Hong Kong

	// Oceania
	"150": "🇦🇺", // Australia
	"170": "🇳🇿", // New Zealand
	"176": "🇫🇯", // Fiji
	"163": "🇵🇬", // Papua New Guinea
	"158": "🇻🇺", // Vanuatu
	"162": "🇳🇨", // New Caledonia
	"175": "🇵🇫", // Tahiti

	// Africa
	"462": "🇿🇦", // South Africa
	"478": "🇪🇬", // Egypt
	"430": "🇰🇪", // Kenya
	"450": "🇳🇬", // Nigeria
	"446": "🇲🇦", // Morocco
	"474": "🇹🇳", // Tunisia
	"438": "🇲🇬", // Madagascar
	"453": "🇷🇪", // Reunion
	"165": "🇲🇺", // Mauritius

	// Special/Antarctic
	"13":  "🇦🇶", // Antarctica
	"460": "",   // Rotuma I. (no standard flag)
	"901": "",   // Test DX1 Entity placeholder
}
