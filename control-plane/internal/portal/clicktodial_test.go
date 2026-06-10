package portal

import (
	"strings"
	"testing"
)

func TestSanitizeDialTarget(t *testing.T) {
	valid := []struct{ in, want string }{
		{"4155551234", "4155551234"},
		{"+14155551234", "+14155551234"},
		{" +14155551234 ", "+14155551234"}, // trimmed
		{"1001", "1001"},                   // internal extension
		{"*97", "*97"},                     // feature code
		{"#", "#"},
		{"*67#", "*67#"},
	}
	for _, c := range valid {
		got, ok := sanitizeDialTarget(c.in)
		if !ok {
			t.Errorf("sanitizeDialTarget(%q) rejected; want %q", c.in, c.want)
			continue
		}
		if got != c.want {
			t.Errorf("sanitizeDialTarget(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	junk := []string{
		"",                       // empty
		"   ",                    // whitespace only
		"+",                      // bare plus
		"abc",                    // letters
		"415-555-1234",           // dashes
		"415 555 1234",           // spaces
		"4155551234;hangup",      // app injection
		"sip:evil@x",             // sip uri
		"+1(415)5551234",         // parens
		"4155551234@example.com", // domain
		"+1+2",                   // plus not at start
		"$(whoami)",              // shell
		"4155551234\nhangup",     // newline
	}
	for _, j := range junk {
		if got, ok := sanitizeDialTarget(j); ok {
			t.Errorf("sanitizeDialTarget(%q) = %q, ok=true; want rejected", j, got)
		}
	}
}

func TestBuildClickToDialActions(t *testing.T) {
	got := buildClickToDialActions("+14155551234")
	// Must be a transfer back into the XML dialplan with the target number.
	for _, want := range []string{"&transfer(", "+14155551234", "XML default"} {
		if !strings.Contains(got, want) {
			t.Errorf("buildClickToDialActions = %q, missing %q", got, want)
		}
	}
	// The whole app must be exactly the transfer form (no extra apps appended).
	if got != "&transfer(+14155551234 XML default)" {
		t.Errorf("unexpected app string: %q", got)
	}

	// An internal extension target works the same way.
	if got := buildClickToDialActions("1001"); got != "&transfer(1001 XML default)" {
		t.Errorf("internal target app = %q", got)
	}
}
