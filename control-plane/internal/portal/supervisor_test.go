package portal

import (
	"strings"
	"testing"
)

func TestBuildEavesdropApp(t *testing.T) {
	const target = "11112222-3333-4444-5555-666677778888"

	tests := []struct {
		mode        string
		mustContain []string
		mustNot     []string
	}{
		{
			mode: "monitor",
			// Pure listen: dtmf enabled, no whisper talk vars.
			mustContain: []string{"eavesdrop_enable_dtmf=true", "&eavesdrop:" + target},
			mustNot:     []string{"whisper_aleg", "whisper_bleg"},
		},
		{
			mode: "whisper",
			// Audible to the agent (A-leg) only — not the far party (B-leg).
			mustContain: []string{"eavesdrop_whisper_aleg=true", "&eavesdrop:" + target},
			mustNot:     []string{"whisper_bleg"},
		},
		{
			mode: "barge",
			// Audible to both legs (three-way).
			mustContain: []string{"eavesdrop_whisper_aleg=true", "eavesdrop_whisper_bleg=true", "&eavesdrop:" + target},
		},
	}

	apps := map[string]string{}
	for _, tc := range tests {
		got := buildEavesdropApp(target, tc.mode)
		apps[tc.mode] = got

		// Every mode must target the call's channel UUID.
		if !strings.Contains(got, target) {
			t.Errorf("mode %q: app %q missing target uuid", tc.mode, got)
		}
		// Every mode allows live DTMF mode-switching.
		if !strings.Contains(got, "eavesdrop_enable_dtmf=true") {
			t.Errorf("mode %q: app %q missing eavesdrop_enable_dtmf", tc.mode, got)
		}
		for _, want := range tc.mustContain {
			if !strings.Contains(got, want) {
				t.Errorf("mode %q: app %q missing %q", tc.mode, got, want)
			}
		}
		for _, bad := range tc.mustNot {
			if strings.Contains(got, bad) {
				t.Errorf("mode %q: app %q should not contain %q", tc.mode, got, bad)
			}
		}
	}

	// Each mode must emit a distinct app string so the box behaves differently.
	if apps["monitor"] == apps["whisper"] ||
		apps["whisper"] == apps["barge"] ||
		apps["monitor"] == apps["barge"] {
		t.Errorf("modes must produce distinct app strings: %+v", apps)
	}
}

func TestSupervisorModesAllowList(t *testing.T) {
	for _, ok := range []string{"monitor", "whisper", "barge"} {
		if !supervisorModes[ok] {
			t.Errorf("mode %q should be allowed", ok)
		}
	}
	for _, bad := range []string{"", "spy", "MONITOR", "hangup", "eavesdrop"} {
		if supervisorModes[bad] {
			t.Errorf("mode %q should be rejected", bad)
		}
	}
}
