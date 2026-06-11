package freeswitch

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/smtp"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// isVoicemailLeaveMessage returns true for the mod_voicemail custom event
// that fires when a caller finishes recording a new message.
func isVoicemailLeaveMessage(e eventLike) bool {
	if e.GetName() != "CUSTOM" {
		return false
	}
	if e.GetHeader("Event-Subclass") != "voicemail::maintenance" {
		return false
	}
	return e.GetHeader("VM-Action") == "leave-message"
}

// handleVoicemailLeaveMessage persists the message row and (if SMTP +
// mailbox email are configured) sends a notification email with the
// recording attached.
//
// Best-effort: any failure logs and returns; nothing fatal.
func (c *ESLClient) handleVoicemailLeaveMessage(ev eventLike) {
	user := ev.GetHeader("VM-User")
	domain := ev.GetHeader("VM-Domain")
	audioPath := ev.GetHeader("VM-File-Path")
	callerNum := ev.GetHeader("VM-Caller-ID-Number")
	callerName := ev.GetHeader("VM-Caller-ID-Name")
	durationSec, _ := strconv.Atoi(ev.GetHeader("VM-Message-Len"))

	slog.Info("voicemail recorded",
		"user", user, "domain", domain,
		"caller", callerNum, "duration_sec", durationSec,
		"audio_path", audioPath,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	box, err := c.store.GetVoicemailBoxByUserDomain(ctx, user, domain)
	if err != nil {
		slog.Error("vm box lookup", "user", user, "domain", domain, "err", err)
		return
	}

	if err := c.store.CreateVoicemailMessage(ctx, store.CreateVoicemailMessageInput{
		BoxID:        box.ID,
		CallerIDNum:  callerNum,
		CallerIDName: callerName,
		DurationSec:  durationSec,
		AudioPath:    audioPath,
	}); err != nil {
		slog.Error("vm message insert", "err", err)
	}

	c.webhooks.Fire(box.TenantID, "voicemail.new", map[string]any{
		"extension_id":   box.ExtensionID.String(),
		"user":           user,
		"domain":         domain,
		"caller_id_num":  callerNum,
		"caller_id_name": callerName,
		"duration_sec":   durationSec,
	})

	// VM-to-email notification. Gate: opt-in flag set on the box AND a valid
	// recipient address AND SMTP configured. Any of those missing → silently
	// skip (the feature ships inert until an extension opts in and SMTP is wired).
	if !shouldSendVoicemailEmail(box, c.mailer.Configured()) {
		return
	}

	// Send email in its own goroutine so a slow SMTP relay can't block the
	// ESL event loop / the call path. Best-effort: failures log, never propagate.
	go c.sendVoicemailEmail(box, user, callerNum, callerName, durationSec, audioPath)
}

// shouldSendVoicemailEmail is the pure gating predicate for the VM-to-email
// notification: the box must have opted in, carry a plausible recipient address
// (non-empty + contains '@'), and SMTP must be configured. Extracted so the
// decision is unit-testable without an SMTP relay / ESL session.
func shouldSendVoicemailEmail(box *store.VoicemailBox, smtpConfigured bool) bool {
	if box == nil || !box.EmailEnabled {
		return false
	}
	if box.EmailAddress == "" || !strings.Contains(box.EmailAddress, "@") {
		return false
	}
	return smtpConfigured
}

func (c *ESLClient) sendVoicemailEmail(box *store.VoicemailBox, extension, callerNum, callerName string, durationSec int, audioPath string) {
	// Attaching the recording is OPTIONAL: only attach when the bytes are
	// trivially readable from the control-plane (shared volume). Otherwise the
	// email just links to the portal inbox.
	var attachments []smtp.Attachment
	if audioPath != "" {
		if data, err := os.ReadFile(audioPath); err == nil {
			attachments = append(attachments, smtp.Attachment{
				Filename:    filepath.Base(audioPath),
				ContentType: contentTypeForAudio(audioPath),
				Data:        data,
			})
		} else {
			slog.Debug("vm audio not readable from control-plane; linking to portal only",
				"path", audioPath, "err", err)
		}
	}

	inboxURL := c.voicemailInboxURL(box.ExtensionID)
	if err := c.mailer.SendVoicemailNotification(smtp.VoicemailNotification{
		To:           box.EmailAddress,
		CallerName:   callerName,
		CallerNumber: callerNum,
		Extension:    extension,
		ReceivedAt:   time.Now(),
		DurationSec:  durationSec,
		InboxURL:     inboxURL,
		Attachments:  attachments,
	}); err != nil {
		slog.Error("vm email send", "to", box.EmailAddress, "err", err)
		return
	}
	slog.Info("vm email sent", "to", box.EmailAddress, "from", callerNum, "duration_sec", durationSec)
}

// voicemailInboxURL builds the owner-facing self-service inbox link for an
// extension. Falls back to a bare path when no portal base URL is configured.
func (c *ESLClient) voicemailInboxURL(extID uuid.UUID) string {
	path := "/admin/me/extensions/" + extID.String()
	base := strings.TrimRight(c.portalBaseURL, "/")
	if base == "" {
		return path
	}
	return base + path
}

func contentTypeForAudio(path string) string {
	switch filepath.Ext(path) {
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".ogg":
		return "audio/ogg"
	default:
		return "application/octet-stream"
	}
}
