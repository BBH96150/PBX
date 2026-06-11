package smtp

import (
	"strings"
	"testing"
	"time"
)

// An unconfigured Mailer must be a no-op (return nil without attempting a send),
// mirroring SendInvite/SendPasswordReset. This is what keeps the feature inert
// when SMTP isn't wired.
func TestSendVoicemailNotification_NoopWhenUnconfigured(t *testing.T) {
	var m Mailer // zero value → not Configured()
	if m.Configured() {
		t.Fatal("zero Mailer should not be Configured()")
	}
	err := m.SendVoicemailNotification(VoicemailNotification{
		To:           "owner@example.com",
		CallerNumber: "5555551234",
		Extension:    "1001",
		ReceivedAt:   time.Now(),
		DurationSec:  42,
		InboxURL:     "https://app.example.com/admin/me/extensions/x",
	})
	if err != nil {
		t.Fatalf("unconfigured SendVoicemailNotification should no-op nil, got %v", err)
	}
}

func TestVoicemailNotificationContent(t *testing.T) {
	when := time.Date(2026, 6, 10, 15, 4, 5, 0, time.UTC)
	subj, body := voicemailNotificationContent(VoicemailNotification{
		CallerName:   "Alice",
		CallerNumber: "5555551234",
		Extension:    "1001",
		ReceivedAt:   when,
		DurationSec:  75,
		InboxURL:     "https://app.example.com/admin/me/extensions/abc",
	})
	if subj != "New voicemail from Alice (5555551234)" {
		t.Errorf("subject: got %q", subj)
	}
	for _, want := range []string{
		"extension 1001",
		"Alice (5555551234)",
		"1m 15s", // 75s formatted
		"https://app.example.com/admin/me/extensions/abc",
		"— SIP Platform",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n---\n%s", want, body)
		}
	}
	// No attachment → no "attached" note.
	if strings.Contains(body, "attached") {
		t.Errorf("body should not mention attachment when none present:\n%s", body)
	}
}

func TestVoicemailNotificationContent_NumberOnlyCaller(t *testing.T) {
	subj, _ := voicemailNotificationContent(VoicemailNotification{
		CallerNumber: "5555551234",
		// CallerName empty → subject/from should be just the number.
		Extension:  "1001",
		ReceivedAt: time.Now(),
	})
	if subj != "New voicemail from 5555551234" {
		t.Errorf("subject: got %q", subj)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[int]string{
		0:   "0s",
		5:   "5s",
		59:  "59s",
		60:  "1m 0s",
		75:  "1m 15s",
		125: "2m 5s",
		-3:  "0s",
	}
	for in, want := range cases {
		if got := formatDuration(in); got != want {
			t.Errorf("formatDuration(%d) = %q, want %q", in, got, want)
		}
	}
}
