package freeswitch

import (
	"testing"
	"time"
)

func TestNormalizeDirection(t *testing.T) {
	cases := map[string]string{
		"inbound": "inbound", "OUTBOUND": "outbound", "Internal": "internal",
		"weird": "internal", "": "internal",
	}
	for in, want := range cases {
		if got := normalizeDirection(in); got != want {
			t.Errorf("normalizeDirection(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeDisposition(t *testing.T) {
	cases := map[string]string{
		"answer": "ANSWERED", "ANSWERED": "ANSWERED",
		"noanswer": "NO_ANSWER", "NO_ANSWER": "NO_ANSWER",
		"user_busy": "BUSY", "BUSY": "BUSY",
		"originator_cancel": "CANCELLED", "cancelled": "CANCELLED",
		"congestion": "CONGESTION",
		"":           "FAILED", "gibberish": "FAILED",
	}
	for in, want := range cases {
		if got := normalizeDisposition(in); got != want {
			t.Errorf("normalizeDisposition(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseEpoch(t *testing.T) {
	if !parseEpoch("").IsZero() || !parseEpoch("0").IsZero() || !parseEpoch("notnum").IsZero() {
		t.Error("empty/zero/invalid epoch should be zero time")
	}
	got := parseEpoch("1700000000")
	want := time.Unix(1700000000, 0).UTC()
	if !got.Equal(want) {
		t.Errorf("parseEpoch = %v, want %v", got, want)
	}
	if got.Location() != time.UTC {
		t.Error("parseEpoch should return UTC")
	}
}

func TestDecodeFSValue(t *testing.T) {
	cases := map[string]string{
		"Smith%20J": "Smith J",
		"plain":     "plain",
		"":          "",
		"a%2Bb":     "a+b",
	}
	for in, want := range cases {
		if got := decodeFSValue(in); got != want {
			t.Errorf("decodeFSValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeSMSNumber(t *testing.T) {
	if got := normalizeSMSNumber("  (415) 555-0123 "); got != "+14155550123" {
		t.Errorf("US number = %q, want +14155550123", got)
	}
	if got := normalizeSMSNumber(""); got != "" {
		t.Errorf("empty = %q, want empty", got)
	}
	// Non-normalizable input is returned trimmed, unchanged.
	if got := normalizeSMSNumber("shortcode"); got != "shortcode" {
		t.Errorf("non-e164 = %q, want passthrough", got)
	}
}

func TestSafeFileBase(t *testing.T) {
	cases := map[string]string{
		"acme-callcentric_1": "acme-callcentric_1",
		"../etc/passwd":      "etcpasswd",
		"a b/c.d":            "abcd",
		"":                   "",
	}
	for in, want := range cases {
		if got := safeFileBase(in); got != want {
			t.Errorf("safeFileBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitStatusLine(t *testing.T) {
	k, v, ok := splitStatusLine("Status\tREGED")
	if !ok || k != "Status" || v != "REGED" {
		t.Errorf("tab split = (%q,%q,%v)", k, v, ok)
	}
	k, v, ok = splitStatusLine("Name   acme gw")
	if !ok || k != "Name" || v != "acme gw" {
		t.Errorf("space split = (%q,%q,%v)", k, v, ok)
	}
	if _, _, ok := splitStatusLine("noseparator"); ok {
		t.Error("line without separator should be !ok")
	}
	if _, _, ok := splitStatusLine("Key   "); ok {
		t.Error("empty value should be !ok")
	}
}
