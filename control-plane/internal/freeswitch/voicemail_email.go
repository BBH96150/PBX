package freeswitch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

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

	if box.Email == "" {
		return
	}
	if !c.mailer.Configured() {
		slog.Debug("vm box has email but smtp not configured; skipping send", "box", box.ID)
		return
	}

	// Send email in its own goroutine so a slow SMTP relay can't block the
	// ESL event loop.
	go c.sendVoicemailEmail(box, callerNum, callerName, durationSec, audioPath)
}

func (c *ESLClient) sendVoicemailEmail(box *store.VoicemailBox, callerNum, callerName string, durationSec int, audioPath string) {
	var attachments []smtp.Attachment
	if data, err := os.ReadFile(audioPath); err == nil {
		attachments = append(attachments, smtp.Attachment{
			Filename:    filepath.Base(audioPath),
			ContentType: contentTypeForAudio(audioPath),
			Data:        data,
		})
	} else {
		slog.Warn("vm audio file unreadable from control-plane (volume not mounted?)",
			"path", audioPath, "err", err)
	}

	if callerName == "" {
		callerName = callerNum
	}
	subject := fmt.Sprintf("New voicemail from %s", callerNum)
	body := fmt.Sprintf(
		"You have a new voicemail.\n\nFrom: %s <%s>\nDuration: %d seconds\nReceived: %s\n\n",
		callerName, callerNum, durationSec, time.Now().UTC().Format(time.RFC1123),
	)
	if len(attachments) == 0 {
		body += "Audio file was not readable by the control-plane. The recording is at " + audioPath + " on the FreeSWITCH host."
	} else {
		body += "The recording is attached."
	}

	if err := c.mailer.Send(box.Email, subject, body, attachments); err != nil {
		slog.Error("vm email send", "to", box.Email, "err", err)
		return
	}
	slog.Info("vm email sent", "to", box.Email, "from", callerNum, "duration_sec", durationSec)
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
