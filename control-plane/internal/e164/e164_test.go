package e164

import "testing"

func TestNormalize(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  bool
	}{
		{"+15555551234", "+15555551234", false},
		{"15555551234", "+15555551234", false},
		{"5555551234", "+15555551234", false},
		{"(555) 555-1234", "+15555551234", false},
		{"9 1 555 555 1234", "+15555551234", false},  // trunk-9 + 11-digit
		{"9 5555551234", "+15555551234", false},       // trunk-9 + 10-digit
		{"00441234567890", "+441234567890", false},    // international 00 prefix
		{"+441234567890", "+441234567890", false},
		{"123", "", true},                              // too short
		{"abcdef", "", true},                           // non-digits
		{"+0", "", true},                               // invalid E.164 (leading 0 after +)
	}
	for _, tc := range cases {
		got, err := Normalize(tc.in, "US")
		if tc.err {
			if err == nil {
				t.Errorf("Normalize(%q): expected error, got %q", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("Normalize(%q): unexpected error %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLooksLikeExternal(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"101", false},
		{"9999", false},
		{"5555551234", true},
		{"+15555551234", true},
		{"(555) 555-1234", true},
		{"*97", false}, // star code
		{"", false},
	}
	for _, tc := range cases {
		if got := LooksLikeExternal(tc.in); got != tc.want {
			t.Errorf("LooksLikeExternal(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestDialDigits(t *testing.T) {
	if got := DialDigits("+15555551234"); got != "15555551234" {
		t.Errorf("DialDigits stripped wrong: %s", got)
	}
	if got := DialDigits("15555551234"); got != "15555551234" {
		t.Errorf("DialDigits should leave non-+ alone: %s", got)
	}
}
