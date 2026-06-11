package smtp

import (
	"fmt"
	"strings"
	"time"
)

// VoicemailNotification is the data rendered into a new-voicemail email.
type VoicemailNotification struct {
	To           string    // recipient address
	CallerName   string    // caller id name (may be empty)
	CallerNumber string    // caller id number
	Extension    string    // the called extension number
	ReceivedAt   time.Time // when the message was left
	DurationSec  int       // recording length in seconds
	InboxURL     string    // portal link to the voicemail inbox
	Attachments  []Attachment
}

// SendVoicemailNotification emails the box owner that a new voicemail arrived.
// Mirrors the auth emails (SendInvite/SendPasswordReset): plain-text body built
// with fmt.Sprintf + strings.TrimSpace, gated on Configured().
//
// If the Mailer is not Configured(), this is a no-op that returns nil so callers
// don't have to special-case dev environments.
func (m Mailer) SendVoicemailNotification(n VoicemailNotification) error {
	if !m.Configured() {
		return nil
	}
	subject, body := voicemailNotificationContent(n)
	return m.Send(n.To, subject, body, n.Attachments)
}

// voicemailNotificationContent builds the subject + plain-text body for a
// new-voicemail email. Pure (no I/O) so it's unit-testable.
func voicemailNotificationContent(n VoicemailNotification) (subject, body string) {
	from := n.CallerNumber
	if n.CallerName != "" && n.CallerName != n.CallerNumber {
		from = fmt.Sprintf("%s (%s)", n.CallerName, n.CallerNumber)
	}
	subject = fmt.Sprintf("New voicemail from %s", from)
	body = strings.TrimSpace(fmt.Sprintf(`
Hello,

You have a new voicemail for extension %s.

From:      %s
Received:  %s
Duration:  %s

Listen in your voicemail inbox:

%s

%s— SIP Platform
`,
		n.Extension,
		from,
		n.ReceivedAt.UTC().Format(time.RFC1123),
		formatDuration(n.DurationSec),
		n.InboxURL,
		attachNote(n.Attachments),
	))
	return subject, body
}

// formatDuration renders seconds as "Mm Ss" (or "Ss" under a minute).
func formatDuration(sec int) string {
	if sec < 0 {
		sec = 0
	}
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	return fmt.Sprintf("%dm %ds", sec/60, sec%60)
}

// attachNote adds a trailing line noting the recording is attached, when it is.
func attachNote(att []Attachment) string {
	if len(att) == 0 {
		return ""
	}
	return "The recording is attached to this email.\n\n"
}
