package freeswitch

import (
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeEvent is an eventLike implementation backed by a map for unit tests.
type fakeEvent struct {
	name    string
	headers map[string]string
}

func (f fakeEvent) GetName() string             { return f.name }
func (f fakeEvent) GetHeader(k string) string   { return f.headers[k] }

func TestShouldRecordCDR(t *testing.T) {
	cases := []struct {
		name      string
		direction string
		want      bool
	}{
		{"CHANNEL_HANGUP_COMPLETE", "inbound", true},
		{"CHANNEL_HANGUP_COMPLETE", "outbound", false},
		{"CHANNEL_HANGUP_COMPLETE", "", false},
		{"CHANNEL_HANGUP", "inbound", false},
		{"CHANNEL_CREATE", "inbound", false},
	}
	for _, c := range cases {
		got := shouldRecordCDR(fakeEvent{
			name:    c.name,
			headers: map[string]string{"Caller-Direction": c.direction},
		})
		if got != c.want {
			t.Errorf("name=%s direction=%s: got %v, want %v", c.name, c.direction, got, c.want)
		}
	}
}

func TestEventToCDR_OutboundCall(t *testing.T) {
	tenantID := uuid.New()
	carrierAcctID := uuid.New()

	startEpoch := time.Now().Add(-2 * time.Minute).Unix()
	answerEpoch := startEpoch + 5
	endEpoch := startEpoch + 65

	ev := fakeEvent{
		name: "CHANNEL_HANGUP_COMPLETE",
		headers: map[string]string{
			"Caller-Direction":               "inbound",
			"Unique-ID":                      "abc-123",
			"variable_x_call_direction":      "outbound",
			"variable_x_tenant_id":           tenantID.String(),
			"variable_x_carrier_account_id":  carrierAcctID.String(),
			"variable_x_dialed_e164":         "+15555551234",
			"variable_sip_from_uri":          "sip%3A101%40acme.sip.local",
			"variable_sip_to_uri":            "sip%3A15555551234%40callcentric.com",
			"Caller-Caller-ID-Number":        "101",
			"Caller-Caller-ID-Name":          "Alice%20Smith",
			"Caller-Destination-Number":      "15555551234",
			"variable_start_epoch":           toS(startEpoch),
			"variable_answer_epoch":          toS(answerEpoch),
			"variable_end_epoch":             toS(endEpoch),
			"variable_duration":              "65",
			"variable_billsec":               "60",
			"variable_originate_disposition": "ANSWER",
			"Hangup-Cause":                   "NORMAL_CLEARING",
		},
	}

	cdr := eventToCDR(ev)
	if cdr.CallUUID != "abc-123" {
		t.Errorf("CallUUID: got %q", cdr.CallUUID)
	}
	if cdr.Direction != "outbound" {
		t.Errorf("Direction: got %q", cdr.Direction)
	}
	if cdr.TenantID == nil || *cdr.TenantID != tenantID {
		t.Errorf("TenantID: got %v", cdr.TenantID)
	}
	if cdr.CarrierID == nil || *cdr.CarrierID != carrierAcctID {
		t.Errorf("CarrierID: got %v", cdr.CarrierID)
	}
	if cdr.CallerIDName != "Alice Smith" {
		t.Errorf("CallerIDName URL-decode: got %q", cdr.CallerIDName)
	}
	if cdr.FromURI != "sip:101@acme.sip.local" {
		t.Errorf("FromURI URL-decode: got %q", cdr.FromURI)
	}
	if cdr.DurationSec == nil || *cdr.DurationSec != 65 {
		t.Errorf("DurationSec: got %v", cdr.DurationSec)
	}
	if cdr.BillableSec == nil || *cdr.BillableSec != 60 {
		t.Errorf("BillableSec: got %v", cdr.BillableSec)
	}
	if cdr.Disposition == nil || *cdr.Disposition != "ANSWERED" {
		t.Errorf("Disposition: got %v", cdr.Disposition)
	}
	if cdr.HangupCause != "NORMAL_CLEARING" {
		t.Errorf("HangupCause: got %q", cdr.HangupCause)
	}
	if cdr.StartedAt.Unix() != startEpoch {
		t.Errorf("StartedAt: got %v want epoch %d", cdr.StartedAt, startEpoch)
	}
	if cdr.AnsweredAt == nil || cdr.AnsweredAt.Unix() != answerEpoch {
		t.Errorf("AnsweredAt: got %v", cdr.AnsweredAt)
	}
}

func TestEventToCDR_MissingFieldsDefaults(t *testing.T) {
	// Minimum viable event — should still produce an insertable CDR.
	ev := fakeEvent{
		name: "CHANNEL_HANGUP_COMPLETE",
		headers: map[string]string{
			"Caller-Direction": "inbound",
			"Unique-ID":        "minimal-1",
			"Hangup-Cause":     "NORMAL_CLEARING",
		},
	}
	cdr := eventToCDR(ev)
	if cdr.CallUUID != "minimal-1" {
		t.Fatalf("CallUUID: %q", cdr.CallUUID)
	}
	if cdr.Direction != "internal" {
		t.Errorf("default direction should be internal, got %q", cdr.Direction)
	}
	if cdr.StartedAt.IsZero() {
		t.Errorf("StartedAt should default to now when epoch is missing")
	}
}

func toS(n int64) string { return strconv.FormatInt(n, 10) }
