package freeswitch

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// alertRecipients resolves trunk-down alert recipients dynamically:
// per-trunk override → tenant admins (DB) → global ALERT_EMAIL → none.
// The override/global/none paths don't touch the DB, so they unit-test cleanly.
func TestAlertRecipients(t *testing.T) {
	tid := uuid.New()

	t.Run("override wins, no DB needed", func(t *testing.T) {
		m := &TrunkMonitor{alertEmail: "global@example.com"} // nil store is fine here
		got := m.alertRecipients(context.Background(), &tid, "ops@acme.com")
		if len(got) != 1 || got[0] != "ops@acme.com" {
			t.Fatalf("override path = %v, want [ops@acme.com]", got)
		}
	})

	t.Run("falls back to global when no tenant", func(t *testing.T) {
		m := &TrunkMonitor{alertEmail: "global@example.com"}
		got := m.alertRecipients(context.Background(), nil, "")
		if len(got) != 1 || got[0] != "global@example.com" {
			t.Fatalf("global path = %v, want [global@example.com]", got)
		}
	})

	t.Run("nil when nothing configured", func(t *testing.T) {
		m := &TrunkMonitor{}
		if got := m.alertRecipients(context.Background(), nil, ""); got != nil {
			t.Fatalf("expected nil recipients, got %v", got)
		}
	})
}
