package portal

import "testing"

// validE164Prefix must accept exactly what the outbound_routes.match_prefix
// CHECK constraint accepts (leading + and digits), so the handler never lets
// through a value the DB will reject. Empty string is handled by the caller
// (catch-all) and intentionally not accepted here.
func TestValidE164Prefix(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"+1", true},
		{"+1415", true},
		{"+447911", true},
		{"", false},       // caller treats blank as catch-all, not a prefix
		{"+", false},      // plus with no digits
		{"1415", false},   // missing leading +
		{"+1 415", false}, // space
		{"+1-415", false}, // punctuation
		{"+1a", false},    // letter
		{"++1", false},    // double plus
	}
	for _, c := range cases {
		if got := validE164Prefix(c.in); got != c.want {
			t.Errorf("validE164Prefix(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
