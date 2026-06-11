package freeswitch

import (
	"testing"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

func TestShouldSendVoicemailEmail(t *testing.T) {
	cases := []struct {
		name      string
		box       *store.VoicemailBox
		smtpReady bool
		want      bool
	}{
		{
			name:      "enabled + valid address + smtp configured",
			box:       &store.VoicemailBox{EmailEnabled: true, EmailAddress: "owner@example.com"},
			smtpReady: true,
			want:      true,
		},
		{
			name:      "disabled toggle is inert even with address + smtp",
			box:       &store.VoicemailBox{EmailEnabled: false, EmailAddress: "owner@example.com"},
			smtpReady: true,
			want:      false,
		},
		{
			name:      "enabled but no address",
			box:       &store.VoicemailBox{EmailEnabled: true, EmailAddress: ""},
			smtpReady: true,
			want:      false,
		},
		{
			name:      "enabled but malformed address (no @)",
			box:       &store.VoicemailBox{EmailEnabled: true, EmailAddress: "not-an-email"},
			smtpReady: true,
			want:      false,
		},
		{
			name:      "enabled + address but smtp not configured",
			box:       &store.VoicemailBox{EmailEnabled: true, EmailAddress: "owner@example.com"},
			smtpReady: false,
			want:      false,
		},
		{
			name:      "nil box",
			box:       nil,
			smtpReady: true,
			want:      false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldSendVoicemailEmail(c.box, c.smtpReady); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestIsVoicemailLeaveMessage(t *testing.T) {
	cases := []struct {
		name     string
		ev       fakeEvent
		want     bool
	}{
		{
			name: "leave-message custom event",
			ev: fakeEvent{
				name: "CUSTOM",
				headers: map[string]string{
					"Event-Subclass": "voicemail::maintenance",
					"VM-Action":      "leave-message",
				},
			},
			want: true,
		},
		{
			name: "voicemail check (not leave-message) is ignored",
			ev: fakeEvent{
				name: "CUSTOM",
				headers: map[string]string{
					"Event-Subclass": "voicemail::maintenance",
					"VM-Action":      "check-folder",
				},
			},
		},
		{
			name: "non-voicemail custom event ignored",
			ev: fakeEvent{
				name: "CUSTOM",
				headers: map[string]string{
					"Event-Subclass": "callcenter::info",
				},
			},
		},
		{
			name: "channel event ignored",
			ev: fakeEvent{
				name: "CHANNEL_HANGUP_COMPLETE",
				headers: map[string]string{
					"Caller-Direction": "inbound",
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isVoicemailLeaveMessage(c.ev); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestContentTypeForAudio(t *testing.T) {
	cases := map[string]string{
		"/recordings/2026/foo.wav":  "audio/wav",
		"/recordings/2026/foo.mp3":  "audio/mpeg",
		"/recordings/2026/foo.ogg":  "audio/ogg",
		"/recordings/2026/foo.bin":  "application/octet-stream",
		"/recordings/2026/no-ext":   "application/octet-stream",
	}
	for path, want := range cases {
		if got := contentTypeForAudio(path); got != want {
			t.Errorf("contentTypeForAudio(%q) = %q, want %q", path, got, want)
		}
	}
}
