package digest

import (
	"strings"
	"testing"
	"time"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

func TestBuildDigestBody(t *testing.T) {
	day := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
	rep := store.CallReport{Total: 10, Answered: 8, AvgTalkSec: 95, Inbound: 6, Outbound: 3, Internal: 1}
	body := buildDigestBody("Acme", day, rep)
	for _, want := range []string{"Acme", "Total calls:   10", "Answered:      8 (80%)", "Avg talk time: 95s", "Inbound:       6"} {
		if !strings.Contains(body, want) {
			t.Errorf("digest body missing %q\n%s", want, body)
		}
	}
}

func TestBuildDigestBodyZero(t *testing.T) {
	body := buildDigestBody("Beta", time.Now().UTC(), store.CallReport{})
	if !strings.Contains(body, "Answered:      0 (0%)") {
		t.Errorf("zero-call digest wrong:\n%s", body)
	}
}
