package utils

import "testing"

func TestBandFromFreq(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected string
		desc     string
	}{
		// 160m band (1.8 - 2.0 MHz)
		{1.897, "160m", "160m: random freq 1"},
		{1.943, "160m", "160m: random freq 2"},
		{1.875, "160m", "160m: random freq 3"},
		{1.8001, "160m", "160m: just inside low edge"},
		{1.7999, "Unknown", "160m: just outside low edge"},
		{1.9999, "160m", "160m: just inside high edge"},
		{2.0001, "Unknown", "160m: just outside high edge"},

		// 80m band (3.5 - 4.0 MHz)
		{3.621, "80m", "80m: random freq 1"},
		{3.787, "80m", "80m: random freq 2"},
		{3.925, "80m", "80m: random freq 3"},
		{3.5001, "80m", "80m: just inside low edge"},
		{3.4999, "Unknown", "80m: just outside low edge"},
		{3.9999, "80m", "80m: just inside high edge"},
		{4.0001, "Unknown", "80m: just outside high edge"},

		// 60m band (5.0 - 5.45 MHz)
		{5.167, "60m", "60m: random freq 1"},
		{5.329, "60m", "60m: random freq 2"},
		{5.398, "60m", "60m: random freq 3"},
		{5.0001, "60m", "60m: just inside low edge"},
		{4.9999, "Unknown", "60m: just outside low edge"},
		{5.4499, "60m", "60m: just inside high edge"},
		{5.4501, "Unknown", "60m: just outside high edge"},

		// 40m band (7.0 - 7.3 MHz)
		{7.089, "40m", "40m: random freq 1"},
		{7.197, "40m", "40m: random freq 2"},
		{7.243, "40m", "40m: random freq 3"},
		{7.0001, "40m", "40m: just inside low edge"},
		{6.9999, "Unknown", "40m: just outside low edge"},
		{7.2999, "40m", "40m: just inside high edge"},
		{7.3001, "Unknown", "40m: just outside high edge"},

		// 30m band (10.1 - 10.15 MHz)
		{10.119, "30m", "30m: random freq 1"},
		{10.131, "30m", "30m: random freq 2"},
		{10.143, "30m", "30m: random freq 3"},
		{10.1001, "30m", "30m: just inside low edge"},
		{10.0999, "Unknown", "30m: just outside low edge"},
		{10.1499, "30m", "30m: just inside high edge"},
		{10.1501, "Unknown", "30m: just outside high edge"},

		// 20m band (14.0 - 14.35 MHz)
		{14.157, "20m", "20m: random freq 1"},
		{14.287, "20m", "20m: random freq 2"},
		{14.061, "20m", "20m: random freq 3"},
		{14.0001, "20m", "20m: just inside low edge"},
		{13.9999, "Unknown", "20m: just outside low edge"},
		{14.3499, "20m", "20m: just inside high edge"},
		{14.3501, "Unknown", "20m: just outside high edge"},

		// 17m band (18.068 - 18.168 MHz)
		{18.103, "17m", "17m: random freq 1"},
		{18.127, "17m", "17m: random freq 2"},
		{18.089, "17m", "17m: random freq 3"},
		{18.0681, "17m", "17m: just inside low edge"},
		{18.0679, "Unknown", "17m: just outside low edge"},
		{18.1679, "17m", "17m: just inside high edge"},
		{18.1681, "Unknown", "17m: just outside high edge"},

		// 15m band (21.0 - 21.45 MHz)
		{21.167, "15m", "15m: random freq 1"},
		{21.329, "15m", "15m: random freq 2"},
		{21.087, "15m", "15m: random freq 3"},
		{21.0001, "15m", "15m: just inside low edge"},
		{20.9999, "Unknown", "15m: just outside low edge"},
		{21.4499, "15m", "15m: just inside high edge"},
		{21.4501, "Unknown", "15m: just outside high edge"},

		// 12m band (24.89 - 24.99 MHz)
		{24.923, "12m", "12m: random freq 1"},
		{24.957, "12m", "12m: random freq 2"},
		{24.903, "12m", "12m: random freq 3"},
		{24.8901, "12m", "12m: just inside low edge"},
		{24.8899, "Unknown", "12m: just outside low edge"},
		{24.9899, "12m", "12m: just inside high edge"},
		{24.9901, "Unknown", "12m: just outside high edge"},

		// 10m band (28.0 - 29.7 MHz)
		{28.487, "10m", "10m: random freq 1"},
		{29.123, "10m", "10m: random freq 2"},
		{28.789, "10m", "10m: random freq 3"},
		{28.0001, "10m", "10m: just inside low edge"},
		{27.9999, "Unknown", "10m: just outside low edge"},
		{29.6999, "10m", "10m: just inside high edge"},
		{29.7001, "Unknown", "10m: just outside high edge"},

		// 6m band (50.0 - 54.0 MHz)
		{51.237, "6m", "6m: random freq 1"},
		{52.843, "6m", "6m: random freq 2"},
		{50.567, "6m", "6m: random freq 3"},
		{50.0001, "6m", "6m: just inside low edge"},
		{49.9999, "Unknown", "6m: just outside low edge"},
		{53.9999, "6m", "6m: just inside high edge"},
		{54.0001, "Unknown", "6m: just outside high edge"},

		// 2m band (144.0 - 148.0 MHz)
		{145.387, "2m", "2m: random freq 1"},
		{146.523, "2m", "2m: random freq 2"},
		{144.789, "2m", "2m: random freq 3"},
		{144.0001, "2m", "2m: just inside low edge"},
		{143.9999, "Unknown", "2m: just outside low edge"},
		{147.9999, "2m", "2m: just inside high edge"},
		{148.0001, "Unknown", "2m: just outside high edge"},

		// 70cm band (430.0 - 450.0 MHz)
		{437.523, "70cm", "70cm: random freq 1"},
		{442.987, "70cm", "70cm: random freq 2"},
		{433.789, "70cm", "70cm: random freq 3"},
		{430.0001, "70cm", "70cm: just inside low edge"},
		{429.9999, "Unknown", "70cm: just outside low edge"},
		{449.9999, "70cm", "70cm: just inside high edge"},
		{450.0001, "Unknown", "70cm: just outside high edge"},

		// Invalid inputs
		{"foo", "Unknown", "invalid string"},
		{999, "Unknown", "frequency out of all bands"},
	}

	for _, tt := range tests {
		got := BandFromFreq(tt.input)
		if got != tt.expected {
			t.Errorf("BandFromFreq(%v) [%s] = %q, want %q", tt.input, tt.desc, got, tt.expected)
		}
	}
}
