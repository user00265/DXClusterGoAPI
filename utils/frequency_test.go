package utils

import "testing"

func TestParseFrequency(t *testing.T) {
	cases := []struct {
		in   interface{}
		want int64
		fail bool
	}{
		{"14250", 14250000, false},
		{"14.250", 14250000, false},
		{14250000, 14250000, false},
		{14.25, 14250000, false},
		{"0.01425", 14250000, false},
		{"foo", 0, true},
		{-1, 0, true},
	}
	for _, c := range cases {
		got, err := ParseFrequency(c.in)
		if c.fail {
			if err == nil {
				t.Errorf("ParseFrequency(%v) should fail", c.in)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("ParseFrequency(%v) = %v, %v; want %v, nil", c.in, got, err, c.want)
		}
	}
}

func TestFormatFrequency(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{14250000, "14.250 MHz"},
		{50125000, "50.125 MHz"},
		{2400000000, "2.400 GHz"},
		{1200000, "1.200 MHz"},
		{999, "999 Hz"},
	}
	for _, c := range cases {
		got := FormatFrequency(c.in)
		if got != c.want {
			t.Errorf("FormatFrequency(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
