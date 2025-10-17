package utils

// BandFromFreq converts a frequency to a ham radio band string.
// Accepts frequency in Hz, kHz, or MHz and automatically detects the unit.
func BandFromFreq(frequency float64) string {
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
	case frequencyMHz >= 0.1357 && frequencyMHz <= 0.1378:
		return "2200m"
	case frequencyMHz >= 0.472 && frequencyMHz <= 0.479:
		return "630m"
	case frequencyMHz >= 1.8 && frequencyMHz <= 2.0:
		return "160m"
	case frequencyMHz >= 3.5 && frequencyMHz <= 4.0:
		return "80m"
	case frequencyMHz >= 5.0 && frequencyMHz <= 5.45:
		return "60m"
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
	case frequencyMHz >= 70.0 && frequencyMHz <= 71.0:
		return "4m"
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
