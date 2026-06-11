package portal

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

func TestAuditCSVHeader(t *testing.T) {
	want := []string{
		"created_at", "actor", "event",
		"target", "ip_address", "detail",
	}
	if len(auditCSVHeader) != len(want) {
		t.Fatalf("header length = %d, want %d", len(auditCSVHeader), len(want))
	}
	for i := range want {
		if auditCSVHeader[i] != want[i] {
			t.Errorf("header[%d] = %q, want %q", i, auditCSVHeader[i], want[i])
		}
	}
}

func TestAuditToCSVRecord_FullRow(t *testing.T) {
	// Non-UTC created time must be normalized to UTC RFC3339.
	loc := time.FixedZone("EST", -5*3600)
	created := time.Date(2026, 6, 10, 9, 30, 0, 0, loc)
	targetID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	rec := auditToCSVRecord(store.AuditEntry{
		ActorEmail: "admin@example.com",
		Event:      "extension.created",
		TargetType: "extension",
		TargetID:   &targetID,
		IPAddress:  "203.0.113.7",
		Payload:    json.RawMessage(`{"number":"1000"}`),
		CreatedAt:  created,
	})

	if len(rec) != len(auditCSVHeader) {
		t.Fatalf("record has %d fields, header has %d", len(rec), len(auditCSVHeader))
	}
	want := []string{
		"2026-06-10T14:30:00Z",
		"admin@example.com",
		"extension.created",
		"extension:11111111-1111-1111-1111-111111111111",
		"203.0.113.7",
		`{"number":"1000"}`,
	}
	for i := range want {
		if rec[i] != want[i] {
			t.Errorf("field %d (%s) = %q, want %q", i, auditCSVHeader[i], rec[i], want[i])
		}
	}
}

func TestAuditToCSVRecord_ActorFallback(t *testing.T) {
	// No email → fall back to the actor user id.
	userID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	rec := auditToCSVRecord(store.AuditEntry{
		ActorUserID: &userID,
		Event:       "login",
		CreatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if rec[1] != "22222222-2222-2222-2222-222222222222" {
		t.Errorf("actor = %q, want user id fallback", rec[1])
	}

	// No email, no user id → fall back to the token id.
	tokenID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	rec = auditToCSVRecord(store.AuditEntry{
		ActorTokenID: &tokenID,
		Event:        "tenant.security.update",
		CreatedAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if rec[1] != "token:33333333-3333-3333-3333-333333333333" {
		t.Errorf("actor = %q, want token id fallback", rec[1])
	}
}

func TestAuditToCSVRecord_NullAndEmptyFields(t *testing.T) {
	// No actor, no target, empty ip, nil payload all render as empty strings.
	rec := auditToCSVRecord(store.AuditEntry{
		Event:     "system.event",
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	})

	if rec[0] != "2026-01-02T03:04:05Z" {
		t.Errorf("created_at = %q, want RFC3339", rec[0])
	}
	if rec[1] != "" {
		t.Errorf("actor = %q, want empty", rec[1])
	}
	if rec[3] != "" {
		t.Errorf("target = %q, want empty", rec[3])
	}
	if rec[4] != "" {
		t.Errorf("ip_address = %q, want empty", rec[4])
	}
	if rec[5] != "" {
		t.Errorf("detail = %q, want empty for nil payload", rec[5])
	}
}

func TestAuditToCSVRecord_TargetTypeOnly(t *testing.T) {
	// target_type present but no target_id → just the type, no trailing colon.
	rec := auditToCSVRecord(store.AuditEntry{
		Event:      "tenant.updated",
		TargetType: "tenant",
		CreatedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if rec[3] != "tenant" {
		t.Errorf("target = %q, want %q", rec[3], "tenant")
	}
}

func TestAuditToCSVRecord_FormulaInjectionNeutralized(t *testing.T) {
	// Attacker-influenced actor email beginning with '=' must be prefixed so a
	// spreadsheet treats it as literal text, not a formula.
	rec := auditToCSVRecord(store.AuditEntry{
		ActorEmail: "=HYPERLINK(\"http://evil\")",
		Event:      "login",
		CreatedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if got := rec[1]; got != "'=HYPERLINK(\"http://evil\")" {
		t.Errorf("actor = %q, want leading-quote-escaped", got)
	}
}
