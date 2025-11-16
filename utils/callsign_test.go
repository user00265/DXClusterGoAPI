package utils

import "testing"

func TestNormalizeCallsign(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"K1ABC-#", "K1ABC"},
		{"K1ABC#", "K1ABC"},
		{" k1abc ", "K1ABC"},
		{"K1ABC-", "K1ABC"},
		{"K1ABC", "K1ABC"},
	}
	for _, c := range cases {
		got := NormalizeCallsign(c.in)
		if got != c.want {
			t.Errorf("NormalizeCallsign(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseCallsign(t *testing.T) {
	cases := []struct {
		in, a, b, c string
	}{
		{"K1ABC", "", "K1ABC", ""},
		{"W1/K1ABC", "W1", "K1ABC", ""},
		{"K1ABC/P", "", "K1ABC", "P"},
		{"W1/K1ABC/P", "W1", "K1ABC", "P"},
		{"K1ABC-#", "", "K1ABC", ""},
	}
	for _, c := range cases {
		parsed := ParseCallsign(c.in)
		if parsed.A != c.a || parsed.B != c.b || parsed.C != c.c {
			t.Errorf("ParseCallsign(%q) = {A:%q B:%q C:%q}, want {A:%q B:%q C:%q}", c.in, parsed.A, parsed.B, parsed.C, c.a, c.b, c.c)
		}
	}
}
