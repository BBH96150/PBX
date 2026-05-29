package freeswitch

import (
	"strings"
	"testing"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

func TestBuildCFFallbackActions(t *testing.T) {
	cases := []struct {
		name     string
		ext      *store.Extension
		want     []string // substrings expected (order-insensitive)
		wantNone bool
	}{
		{
			name:     "no fallback configured",
			ext:      &store.Extension{},
			wantNone: true,
		},
		{
			name: "voicemail-only fallback",
			ext:  &store.Extension{VoicemailEnabled: true, SIPUsername: "101"},
			want: []string{"voicemail default acme.sip.local 101"},
		},
		{
			name: "cf_no_answer only",
			ext:  &store.Extension{CFNoAnswer: "+15555551234"},
			want: []string{"transfer", "+15555551234 XML default"},
		},
		{
			name: "cf_busy only, voicemail off",
			ext:  &store.Extension{CFBusy: "200"},
			want: []string{"USER_BUSY", "? 200 :", "_cf_target=", "transfer"},
		},
		{
			name: "cf_busy only, voicemail on",
			ext:  &store.Extension{CFBusy: "200", VoicemailEnabled: true, SIPUsername: "101"},
			want: []string{"USER_BUSY", "? 200 :", "voicemail default acme.sip.local 101"},
		},
		{
			name: "both cf_busy + cf_no_answer",
			ext:  &store.Extension{CFBusy: "200", CFNoAnswer: "+15555551234"},
			want: []string{"USER_BUSY", "? 200 :", "+15555551234", "transfer"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildCFFallbackActions(c.ext, "acme.sip.local")
			if c.wantNone {
				if len(got) != 0 {
					t.Errorf("expected no actions, got %v", got)
				}
				return
			}
			// Concatenate as "app data" for substring matching (mirrors how
			// the action renders in the dialplan XML).
			var concat strings.Builder
			for _, a := range got {
				concat.WriteString(a.App + " " + a.Data + "\n")
			}
			joined := concat.String()
			for _, want := range c.want {
				if !strings.Contains(joined, want) {
					t.Errorf("missing substring %q in:\n%s", want, joined)
				}
			}
		})
	}
}
