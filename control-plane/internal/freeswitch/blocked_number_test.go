package freeswitch

import (
	"strings"
	"testing"
)

// flattenActions joins actions into "app=data\n" lines for substring assertions.
func flattenActions(actions []dialplanAction) string {
	var sb strings.Builder
	for _, a := range actions {
		sb.WriteString(a.App + "=" + a.Data + "\n")
	}
	return sb.String()
}

func TestBuildBlockedCallerActions(t *testing.T) {
	actions := buildBlockedCallerActions()
	flat := flattenActions(actions)

	for _, want := range []string{
		"x_blocked=true",
		"x_call_direction=inbound",
		"hangup=CALL_REJECTED",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("blocked-caller actions missing %q\n--- got ---\n%s", want, flat)
		}
	}

	// We must NOT answer a blocked call — no billable connect / media.
	if strings.Contains(flat, "answer=") {
		t.Errorf("blocked call must not be answered:\n%s", flat)
	}
	// Must not bridge anywhere.
	if strings.Contains(flat, "bridge=") {
		t.Errorf("blocked call must not bridge:\n%s", flat)
	}
	// hangup must be the final action so nothing routes after it.
	if last := actions[len(actions)-1]; last.App != "hangup" {
		t.Errorf("last action should be hangup, got %q", last.App)
	}
}
