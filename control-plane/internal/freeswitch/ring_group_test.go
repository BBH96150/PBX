package freeswitch

import (
	"strings"
	"testing"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

func member(user, domain string, enabled bool) store.RingGroupMember {
	return store.RingGroupMember{
		SIPUsername: user, SIPDomain: domain, Enabled: enabled,
	}
}

func TestBuildRingGroupDialString_Simultaneous(t *testing.T) {
	ms := []store.RingGroupMember{
		member("101", "acme.sip.local", true),
		member("102", "acme.sip.local", true),
		member("103", "acme.sip.local", false), // disabled — skipped
	}
	out := buildRingGroupDialString(ms, "simultaneous", 30, "kamailio:5060")
	if !strings.Contains(out, ",") {
		t.Errorf("simultaneous should use comma separator; got %q", out)
	}
	if strings.Contains(out, "|") {
		t.Errorf("simultaneous should not contain pipe; got %q", out)
	}
	if strings.Count(out, "sofia/internal") != 2 {
		t.Errorf("expected 2 legs after filtering disabled; got %q", out)
	}
	if !strings.Contains(out, "sip:101@acme.sip.local") || !strings.Contains(out, "sip:102@acme.sip.local") {
		t.Errorf("missing expected URIs in %q", out)
	}
	if !strings.Contains(out, "fs_path=sip:kamailio:5060") {
		t.Errorf("missing fs_path; got %q", out)
	}
	if strings.Contains(out, "leg_timeout") {
		t.Errorf("simultaneous shouldn't add per-leg timeout; got %q", out)
	}
}

func TestBuildRingGroupDialString_Sequential(t *testing.T) {
	ms := []store.RingGroupMember{
		member("101", "acme.sip.local", true),
		member("102", "acme.sip.local", true),
	}
	out := buildRingGroupDialString(ms, "sequential", 15, "kamailio:5060")
	if !strings.Contains(out, "|") {
		t.Errorf("sequential should use pipe separator; got %q", out)
	}
	if strings.Contains(out, ",") {
		t.Errorf("sequential should not contain comma; got %q", out)
	}
	if strings.Count(out, "[leg_timeout=15]") != 2 {
		t.Errorf("each leg should have leg_timeout=15; got %q", out)
	}
}

func TestBuildRingGroupDialString_Empty(t *testing.T) {
	if out := buildRingGroupDialString(nil, "simultaneous", 30, "kamailio:5060"); out != "" {
		t.Errorf("nil members: got %q, want empty", out)
	}
	allDisabled := []store.RingGroupMember{
		member("101", "acme.sip.local", false),
	}
	if out := buildRingGroupDialString(allDisabled, "simultaneous", 30, "kamailio:5060"); out != "" {
		t.Errorf("all disabled: got %q, want empty", out)
	}
}

func TestSupportedRingGroupStrategy(t *testing.T) {
	for _, ok := range []string{"simultaneous", "sequential", "round_robin", "random"} {
		if !supportedRingGroupStrategy(ok) {
			t.Errorf("%q should be supported", ok)
		}
	}
	for _, notOk := range []string{"", "bogus", "weighted"} {
		if supportedRingGroupStrategy(notOk) {
			t.Errorf("%q should NOT be supported", notOk)
		}
	}
}

func TestRotateMembers(t *testing.T) {
	ms := []store.RingGroupMember{
		member("101", "d", true),
		member("102", "d", true),
		member("103", "d", true),
	}

	// start=0 → unchanged
	if got := rotateMembers(ms, 0); got[0].SIPUsername != "101" {
		t.Errorf("rotate(0)[0] = %s, want 101", got[0].SIPUsername)
	}
	// start=1 → 102, 103, 101
	got := rotateMembers(ms, 1)
	if got[0].SIPUsername != "102" || got[1].SIPUsername != "103" || got[2].SIPUsername != "101" {
		t.Errorf("rotate(1) order wrong: %s,%s,%s",
			got[0].SIPUsername, got[1].SIPUsername, got[2].SIPUsername)
	}
	// start=2 → 103, 101, 102
	got = rotateMembers(ms, 2)
	if got[0].SIPUsername != "103" || got[1].SIPUsername != "101" || got[2].SIPUsername != "102" {
		t.Errorf("rotate(2) order wrong: %s,%s,%s",
			got[0].SIPUsername, got[1].SIPUsername, got[2].SIPUsername)
	}
	// out-of-range start is wrapped
	got = rotateMembers(ms, 4) // 4 % 3 == 1
	if got[0].SIPUsername != "102" {
		t.Errorf("rotate(4) wrap failed: %s", got[0].SIPUsername)
	}
	got = rotateMembers(ms, -1) // -1 % 3 → 2 after defensive math
	if got[0].SIPUsername != "103" {
		t.Errorf("rotate(-1) wrap failed: %s", got[0].SIPUsername)
	}
	// empty input
	if got := rotateMembers(nil, 5); len(got) != 0 {
		t.Errorf("rotate(nil) should return empty, got %v", got)
	}
}

func TestShuffleMembers_PreservesContents(t *testing.T) {
	ms := []store.RingGroupMember{
		member("101", "d", true),
		member("102", "d", true),
		member("103", "d", true),
		member("104", "d", true),
	}
	got := shuffleMembers(ms)
	if len(got) != len(ms) {
		t.Fatalf("length changed: got %d", len(got))
	}
	// Must contain the same set (order is random; non-deterministic test
	// just verifies the multiset matches).
	want := map[string]bool{"101": true, "102": true, "103": true, "104": true}
	for _, m := range got {
		if !want[m.SIPUsername] {
			t.Errorf("unexpected member after shuffle: %s", m.SIPUsername)
		}
		delete(want, m.SIPUsername)
	}
	if len(want) != 0 {
		t.Errorf("missing members after shuffle: %v", want)
	}
}
