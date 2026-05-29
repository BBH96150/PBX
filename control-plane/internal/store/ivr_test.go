package store

import (
	"testing"

	"github.com/google/uuid"
)

func TestResolveIVRMenuEntry(t *testing.T) {
	subIVR := uuid.New()
	cases := []struct {
		name   string
		in     IVROptionResolved
		action string
		param  string
	}{
		{
			name:   "extension resolved",
			in:     IVROptionResolved{Digit: "1", ActionKind: "extension", ExtNumber: "101"},
			action: "menu-exec-app",
			param:  "transfer 101 XML default",
		},
		{
			name:   "extension orphan FK falls back to hangup",
			in:     IVROptionResolved{Digit: "1", ActionKind: "extension", ExtNumber: ""},
			action: "menu-exit",
		},
		{
			name:   "ring_group resolved",
			in:     IVROptionResolved{Digit: "2", ActionKind: "ring_group", RingGroupExt: "200"},
			action: "menu-exec-app",
			param:  "transfer 200 XML default",
		},
		{
			name:   "ring_group without extension number degrades",
			in:     IVROptionResolved{Digit: "2", ActionKind: "ring_group", RingGroupExt: ""},
			action: "menu-exit",
		},
		{
			name: "voicemail resolved",
			in: IVROptionResolved{
				Digit: "3", ActionKind: "voicemail",
				VoicemailUser: "101", VoicemailDomain: "acme.sip.local",
			},
			action: "menu-exec-app",
			param:  "voicemail default acme.sip.local 101",
		},
		{
			name:   "voicemail missing user degrades",
			in:     IVROptionResolved{Digit: "3", ActionKind: "voicemail", VoicemailDomain: "acme"},
			action: "menu-exit",
		},
		{
			name:   "sub-ivr resolved",
			in:     IVROptionResolved{Digit: "4", ActionKind: "ivr", ActionID: &subIVR},
			action: "menu-exec-app",
			param:  "ivr " + subIVR.String(),
		},
		{
			name:   "sub-ivr without action_id degrades",
			in:     IVROptionResolved{Digit: "4", ActionKind: "ivr", ActionID: nil},
			action: "menu-exit",
		},
		{
			name:   "dial_e164 resolved",
			in:     IVROptionResolved{Digit: "9", ActionKind: "dial_e164", ActionData: "+15555551234"},
			action: "menu-exec-app",
			param:  "transfer +15555551234 XML default",
		},
		{
			name:   "dial_e164 missing data degrades",
			in:     IVROptionResolved{Digit: "9", ActionKind: "dial_e164", ActionData: ""},
			action: "menu-exit",
		},
		{
			name:   "queue resolved",
			in:     IVROptionResolved{Digit: "5", ActionKind: "queue", QueueExt: "500"},
			action: "menu-exec-app",
			param:  "transfer 500 XML default",
		},
		{
			name:   "queue without extension degrades",
			in:     IVROptionResolved{Digit: "5", ActionKind: "queue", QueueExt: ""},
			action: "menu-exit",
		},
		{
			name:   "hangup",
			in:     IVROptionResolved{Digit: "0", ActionKind: "hangup"},
			action: "menu-exit",
		},
		{
			name:   "unknown kind degrades safely",
			in:     IVROptionResolved{Digit: "*", ActionKind: "made_up"},
			action: "menu-exit",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveIVRMenuEntry(c.in)
			if got.Action != c.action {
				t.Errorf("action: got %q want %q", got.Action, c.action)
			}
			if got.Param != c.param {
				t.Errorf("param: got %q want %q", got.Param, c.param)
			}
			if got.Digit != c.in.Digit {
				t.Errorf("digit not preserved: got %q", got.Digit)
			}
		})
	}
}
