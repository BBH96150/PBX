package store

import (
	"regexp"
	"testing"
)

var hexMD5 = regexp.MustCompile(`^[0-9a-f]{32}$`)

func TestComputeHA1(t *testing.T) {
	h := ComputeHA1("1001", "acme.sip.local", "s3cret")
	if !hexMD5.MatchString(h) {
		t.Fatalf("HA1 not 32-char lowercase hex: %q", h)
	}
	// Deterministic.
	if h != ComputeHA1("1001", "acme.sip.local", "s3cret") {
		t.Fatal("HA1 not deterministic")
	}
	// Sensitive to each field.
	if h == ComputeHA1("1002", "acme.sip.local", "s3cret") {
		t.Error("HA1 should change with username")
	}
	if h == ComputeHA1("1001", "other.sip.local", "s3cret") {
		t.Error("HA1 should change with realm")
	}
	if h == ComputeHA1("1001", "acme.sip.local", "different") {
		t.Error("HA1 should change with password")
	}
}

func TestComputeHA1bDiffersFromHA1(t *testing.T) {
	a := ComputeHA1("1001", "acme.sip.local", "pw")
	b := ComputeHA1b("1001", "acme.sip.local", "pw")
	if !hexMD5.MatchString(b) {
		t.Fatalf("HA1b not 32-char hex: %q", b)
	}
	if a == b {
		t.Error("HA1b (username@realm form) should differ from HA1")
	}
}
