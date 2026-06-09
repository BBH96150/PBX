package store

import "testing"

func TestNormalizeMAC(t *testing.T) {
	// All of these are the same MAC in different vendor formats.
	want := "aa:bb:cc:dd:ee:ff"
	for _, in := range []string{
		"aa:bb:cc:dd:ee:ff",
		"AA-BB-CC-DD-EE-FF",
		"aabb.ccdd.eeff", // Cisco dotted
		"AABBCCDDEEFF",
		"  aa bb cc dd ee ff  ",
	} {
		got, err := normalizeMAC(in)
		if err != nil {
			t.Errorf("normalizeMAC(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("normalizeMAC(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeMACErrors(t *testing.T) {
	for _, in := range []string{
		"",
		"aabbccddee",       // too short
		"aabbccddeeffaa",   // too long
		"gg:bb:cc:dd:ee:ff", // non-hex
	} {
		if _, err := normalizeMAC(in); err == nil {
			t.Errorf("normalizeMAC(%q) should have errored", in)
		}
	}
}

func TestInsertColons(t *testing.T) {
	if got := insertColons("aabbccddeeff"); got != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("insertColons = %q", got)
	}
}
