//go:build integration

package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestIntegrationCallInsightCRUDAndPending(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)

	callUUID := "ci-" + uuid.NewString()
	recPath := "/var/lib/freeswitch/recordings/" + callUUID + ".wav"

	// A CDR with a recording but no insight yet.
	cdr := &CDR{
		TenantID:      &ten.ID,
		CallUUID:      callUUID,
		Direction:     "inbound",
		FromURI:       "sip:caller@example.com",
		ToURI:         "sip:1001@pbx.local",
		StartedAt:     time.Now().UTC(),
		HangupCause:   "NORMAL_CLEARING",
		RecordingPath: recPath,
	}
	if err := s.CreateCDR(ctx, cdr); err != nil {
		t.Fatalf("CreateCDR: %v", err)
	}

	// It should show up as pending (recording present, no insight).
	pending, err := s.ListCDRsPendingInsight(ctx, ten.ID, 50)
	if err != nil {
		t.Fatalf("ListCDRsPendingInsight: %v", err)
	}
	if !containsPending(pending, callUUID) {
		t.Fatalf("expected %s in pending list, got %+v", callUUID, pending)
	}
	if p := pending[0]; p.RecordingPath == "" {
		t.Fatal("pending CDR recording_path should be non-empty")
	}

	// Tenant should appear in the distinct pending-tenant list.
	tids, err := s.ListTenantIDsWithPendingInsight(ctx, 100)
	if err != nil {
		t.Fatalf("ListTenantIDsWithPendingInsight: %v", err)
	}
	if !containsUUID(tids, ten.ID) {
		t.Fatalf("tenant %s should be in pending-tenant list", ten.ID)
	}

	// Create the insight.
	if err := s.CreateCallInsight(ctx, &ten.ID, callUUID, "the transcript", "a summary", []string{"call back", "email invoice"}); err != nil {
		t.Fatalf("CreateCallInsight: %v", err)
	}

	// Now it's no longer pending.
	pending2, _ := s.ListCDRsPendingInsight(ctx, ten.ID, 50)
	if containsPending(pending2, callUUID) {
		t.Fatal("CDR should no longer be pending after insight created")
	}

	// Fetch by call_uuid.
	ci, err := s.GetCallInsightByCallUUID(ctx, ten.ID, callUUID)
	if err != nil {
		t.Fatalf("GetCallInsightByCallUUID: %v", err)
	}
	if ci.Summary != "a summary" || ci.Transcript != "the transcript" || len(ci.ActionItems) != 2 {
		t.Fatalf("unexpected insight: %+v", ci)
	}
	if ci.ActionItems[0] != "call back" {
		t.Fatalf("action item mismatch: %+v", ci.ActionItems)
	}

	// Fetch by CDR id (joins on call_uuid).
	ci2, err := s.GetCallInsightForCDR(ctx, ten.ID, cdr.idFromDB(t, s, callUUID))
	if err != nil {
		t.Fatalf("GetCallInsightForCDR: %v", err)
	}
	if ci2.CallUUID != callUUID {
		t.Fatalf("GetCallInsightForCDR call_uuid mismatch: %q", ci2.CallUUID)
	}

	// Map keyed by CDR id.
	byCDR, err := s.ListCallInsightsByCDRForTenant(ctx, ten.ID, 200)
	if err != nil {
		t.Fatalf("ListCallInsightsByCDRForTenant: %v", err)
	}
	if got, ok := byCDR[cdr.idFromDB(t, s, callUUID)]; !ok || got.Summary != "a summary" {
		t.Fatalf("insight map missing or wrong: %+v", byCDR)
	}

	// ON CONFLICT DO NOTHING: a second create with a different summary is a no-op.
	if err := s.CreateCallInsight(ctx, &ten.ID, callUUID, "x", "DIFFERENT", nil); err != nil {
		t.Fatalf("CreateCallInsight (conflict): %v", err)
	}
	ciAfter, _ := s.GetCallInsightByCallUUID(ctx, ten.ID, callUUID)
	if ciAfter.Summary != "a summary" {
		t.Fatalf("conflict insert should not clobber; got %q", ciAfter.Summary)
	}

	// Cross-tenant isolation: another tenant can't read it.
	other := makeTenant(t, s)
	if _, err := s.GetCallInsightByCallUUID(ctx, other.ID, callUUID); err == nil {
		t.Fatal("cross-tenant GetCallInsightByCallUUID should miss")
	}
}

func TestIntegrationCallInsightNilActionItems(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)
	callUUID := "ci-nil-" + uuid.NewString()

	// nil action items must serialize to a non-NULL empty array (NULL-safe).
	if err := s.CreateCallInsight(ctx, &ten.ID, callUUID, "", "", nil); err != nil {
		t.Fatalf("CreateCallInsight(nil items): %v", err)
	}
	ci, err := s.GetCallInsightByCallUUID(ctx, ten.ID, callUUID)
	if err != nil {
		t.Fatalf("GetCallInsightByCallUUID: %v", err)
	}
	if ci.ActionItems == nil {
		t.Fatal("action_items should decode to non-nil empty slice")
	}
	if ci.Transcript != "" || ci.Summary != "" {
		t.Fatalf("empty transcript/summary expected, got %+v", ci)
	}
}

func TestIntegrationVoicemailTranscript(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)
	ext, _ := makeExtension(t, s, ten, "9301")

	box, err := s.CreateVoicemailBox(ctx, CreateVoicemailBoxInput{
		TenantID: ten.ID, ExtensionID: ext.ID, PIN: "1234",
	})
	if err != nil {
		t.Fatalf("CreateVoicemailBox: %v", err)
	}
	audioPath := fmt.Sprintf("/var/lib/freeswitch/storage/%s.wav", uuid.NewString())
	if err := s.CreateVoicemailMessage(ctx, CreateVoicemailMessageInput{
		BoxID: box.ID, CallerIDNum: "+14155550100", DurationSec: 12, AudioPath: audioPath,
	}); err != nil {
		t.Fatalf("CreateVoicemailMessage: %v", err)
	}
	msgs, err := s.ListVoicemailMessagesForBox(ctx, box.ID)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("ListVoicemailMessagesForBox: %d msgs, err %v", len(msgs), err)
	}
	msgID := msgs[0].ID

	// Pending transcription: has audio, no transcript yet.
	pend, err := s.ListVoicemailMessagesPendingTranscript(ctx, 50)
	if err != nil {
		t.Fatalf("ListVoicemailMessagesPendingTranscript: %v", err)
	}
	if !containsPendingVM(pend, msgID) {
		t.Fatalf("message %s should be pending transcript", msgID)
	}

	// Set the transcript.
	if err := s.SetVoicemailTranscript(ctx, msgID, "Hi, please call me back about the order."); err != nil {
		t.Fatalf("SetVoicemailTranscript: %v", err)
	}

	// No longer pending.
	pend2, _ := s.ListVoicemailMessagesPendingTranscript(ctx, 50)
	if containsPendingVM(pend2, msgID) {
		t.Fatal("message should not be pending after transcript set")
	}

	// Read back tenant-scoped.
	tr, err := s.GetVoicemailTranscript(ctx, ten.ID, msgID)
	if err != nil {
		t.Fatalf("GetVoicemailTranscript: %v", err)
	}
	if tr != "Hi, please call me back about the order." {
		t.Fatalf("transcript mismatch: %q", tr)
	}

	// Map for the inbox view.
	m, err := s.ListVoicemailTranscriptsForBox(ctx, box.ID)
	if err != nil {
		t.Fatalf("ListVoicemailTranscriptsForBox: %v", err)
	}
	if m[msgID] == "" {
		t.Fatalf("transcript map missing message %s", msgID)
	}

	// The existing message scan path must remain unaffected (transcript column
	// is read only via the dedicated getters).
	if msgs[0].DurationSec != 12 {
		t.Fatalf("voicemail scan path changed: %+v", msgs[0])
	}
}

// idFromDB resolves a CDR's id by call_uuid (CreateCDR doesn't return it).
func (c *CDR) idFromDB(t *testing.T, s *Store, callUUID string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := s.DB.QueryRow(context.Background(),
		`SELECT id FROM cdrs WHERE call_uuid=$1`, callUUID).Scan(&id); err != nil {
		t.Fatalf("idFromDB: %v", err)
	}
	return id
}

func containsPending(ps []PendingInsightCDR, callUUID string) bool {
	for _, p := range ps {
		if p.CallUUID == callUUID {
			return true
		}
	}
	return false
}

func containsPendingVM(ps []PendingTranscriptMessage, id uuid.UUID) bool {
	for _, p := range ps {
		if p.ID == id {
			return true
		}
	}
	return false
}

func containsUUID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}
