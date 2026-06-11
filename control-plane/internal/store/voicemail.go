package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrVoicemailMessageNotFound is returned when a tenant-scoped message op misses.
var ErrVoicemailMessageNotFound = errors.New("voicemail message not found for this tenant")

type VoicemailBox struct {
	ID                uuid.UUID `json:"id"`
	TenantID          uuid.UUID `json:"tenant_id"`
	ExtensionID       uuid.UUID `json:"extension_id"`
	PIN               string    `json:"pin,omitempty"`
	Email             string    `json:"email,omitempty"`
	Timezone          string    `json:"timezone"`
	MaxMessages       int       `json:"max_messages"`
	MaxMsgDurationSec int       `json:"max_msg_duration_sec"`
	GreetingPath      string    `json:"greeting_path,omitempty"`
	Enabled           bool      `json:"enabled"`
	// VM-to-email notification opt-in (migration 0043). When EmailEnabled is
	// true and EmailAddress is non-empty, a new voicemail triggers a best-effort
	// notification email. Independent of the legacy Email field.
	EmailEnabled bool   `json:"email_enabled"`
	EmailAddress string `json:"email_address,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateVoicemailBoxInput struct {
	TenantID          uuid.UUID
	ExtensionID       uuid.UUID
	PIN               string
	Email             string
	Timezone          string
	MaxMessages       int
	MaxMsgDurationSec int
}

func (s *Store) CreateVoicemailBox(ctx context.Context, in CreateVoicemailBoxInput) (*VoicemailBox, error) {
	if in.Timezone == "" {
		in.Timezone = "UTC"
	}
	if in.MaxMessages == 0 {
		in.MaxMessages = 100
	}
	if in.MaxMsgDurationSec == 0 {
		in.MaxMsgDurationSec = 300
	}
	const q = `
		INSERT INTO voicemail_boxes
		    (tenant_id, extension_id, pin, email, timezone,
		     max_messages, max_msg_duration_sec)
		VALUES ($1, $2, $3, NULLIF($4,'')::citext, $5, $6, $7)
		RETURNING id, tenant_id, extension_id, pin, COALESCE(email::text,''),
		          timezone, max_messages, max_msg_duration_sec,
		          COALESCE(greeting_path,''), enabled,
		          voicemail_email_enabled, COALESCE(voicemail_email_address::text,''),
		          created_at, updated_at`
	var b VoicemailBox
	err := s.DB.QueryRow(ctx, q,
		in.TenantID, in.ExtensionID, in.PIN, in.Email, in.Timezone,
		in.MaxMessages, in.MaxMsgDurationSec,
	).Scan(
		&b.ID, &b.TenantID, &b.ExtensionID, &b.PIN, &b.Email,
		&b.Timezone, &b.MaxMessages, &b.MaxMsgDurationSec,
		&b.GreetingPath, &b.Enabled,
		&b.EmailEnabled, &b.EmailAddress,
		&b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	// Also flip the extension's voicemail_enabled flag so the dialplan
	// handler knows to use it on no-answer fallback.
	_, _ = s.DB.Exec(ctx, `UPDATE extensions SET voicemail_enabled=true WHERE id=$1`, in.ExtensionID)
	return &b, nil
}

// GetVoicemailBoxByUserDomain resolves (sip_username, sip_domain) → box.
// Used by the ESL VM-to-email handler when a voicemail::leave-message event
// arrives — events carry user+domain, not extension UUID.
func (s *Store) GetVoicemailBoxByUserDomain(ctx context.Context, user, domain string) (*VoicemailBox, error) {
	const q = `
		SELECT vb.id, vb.tenant_id, vb.extension_id, vb.pin, COALESCE(vb.email::text,''),
		       vb.timezone, vb.max_messages, vb.max_msg_duration_sec,
		       COALESCE(vb.greeting_path,''), vb.enabled,
		       vb.voicemail_email_enabled, COALESCE(vb.voicemail_email_address::text,''),
		       vb.created_at, vb.updated_at
		  FROM voicemail_boxes vb
		  JOIN extensions  e  ON e.id  = vb.extension_id
		  JOIN sip_domains sd ON sd.id = e.sip_domain_id
		 WHERE e.sip_username = $1 AND sd.domain = $2
		   AND vb.enabled = true AND e.status = 'active'
		 LIMIT 1`
	var b VoicemailBox
	err := s.DB.QueryRow(ctx, q, user, domain).Scan(
		&b.ID, &b.TenantID, &b.ExtensionID, &b.PIN, &b.Email,
		&b.Timezone, &b.MaxMessages, &b.MaxMsgDurationSec,
		&b.GreetingPath, &b.Enabled,
		&b.EmailEnabled, &b.EmailAddress,
		&b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// CreateVoicemailMessage inserts a metadata row for a freshly-recorded
// message. The audio file itself lives on the FS host at audio_path.
func (s *Store) CreateVoicemailMessage(ctx context.Context, in CreateVoicemailMessageInput) error {
	const q = `
		INSERT INTO voicemail_messages
		    (box_id, caller_id_num, caller_id_name, received_at,
		     duration_sec, audio_path, status)
		VALUES ($1, NULLIF($2,''), NULLIF($3,''), now(),
		        $4, $5, 'new')`
	_, err := s.DB.Exec(ctx, q,
		in.BoxID, in.CallerIDNum, in.CallerIDName,
		in.DurationSec, in.AudioPath,
	)
	return err
}

type CreateVoicemailMessageInput struct {
	BoxID        uuid.UUID
	CallerIDNum  string
	CallerIDName string
	DurationSec  int
	AudioPath    string
}

// VoicemailMessage is one recorded message in a box, as shown in the portal
// inbox.
type VoicemailMessage struct {
	ID           uuid.UUID  `json:"id"`
	BoxID        uuid.UUID  `json:"box_id"`
	CallerIDNum  string     `json:"caller_id_num,omitempty"`
	CallerIDName string     `json:"caller_id_name,omitempty"`
	ReceivedAt   time.Time  `json:"received_at"`
	DurationSec  int        `json:"duration_sec"`
	AudioPath    string     `json:"-"` // host path; never serialized to clients
	Status       string     `json:"status"`
	PlayedAt     *time.Time `json:"played_at,omitempty"`
}

// ListVoicemailMessagesForBox returns the non-deleted messages in a box,
// newest first.
func (s *Store) ListVoicemailMessagesForBox(ctx context.Context, boxID uuid.UUID) ([]VoicemailMessage, error) {
	const q = `
		SELECT id, box_id, COALESCE(caller_id_num,''), COALESCE(caller_id_name,''),
		       received_at, COALESCE(duration_sec,0), audio_path, status, played_at
		  FROM voicemail_messages
		 WHERE box_id = $1 AND status <> 'deleted'
		 ORDER BY received_at DESC`
	rows, err := s.DB.Query(ctx, q, boxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VoicemailMessage
	for rows.Next() {
		var m VoicemailMessage
		if err := rows.Scan(
			&m.ID, &m.BoxID, &m.CallerIDNum, &m.CallerIDName,
			&m.ReceivedAt, &m.DurationSec, &m.AudioPath, &m.Status, &m.PlayedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetVoicemailMessageForTenant fetches one message, enforcing that it belongs
// to the given tenant (via box → tenant). Used before streaming/deleting so a
// user can't reach another tenant's recording by guessing a message ID.
func (s *Store) GetVoicemailMessageForTenant(ctx context.Context, tenantID, msgID uuid.UUID) (*VoicemailMessage, error) {
	const q = `
		SELECT m.id, m.box_id, COALESCE(m.caller_id_num,''), COALESCE(m.caller_id_name,''),
		       m.received_at, COALESCE(m.duration_sec,0), m.audio_path, m.status, m.played_at
		  FROM voicemail_messages m
		  JOIN voicemail_boxes b ON b.id = m.box_id
		 WHERE m.id = $1 AND b.tenant_id = $2 AND m.status <> 'deleted'`
	var m VoicemailMessage
	err := s.DB.QueryRow(ctx, q, msgID, tenantID).Scan(
		&m.ID, &m.BoxID, &m.CallerIDNum, &m.CallerIDName,
		&m.ReceivedAt, &m.DurationSec, &m.AudioPath, &m.Status, &m.PlayedAt,
	)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// MarkVoicemailMessagePlayed stamps played_at (first play) and promotes a
// 'new' message to 'saved'. Tenant-scoped; no-op error if it misses.
func (s *Store) MarkVoicemailMessagePlayed(ctx context.Context, tenantID, msgID uuid.UUID) error {
	const q = `
		UPDATE voicemail_messages m
		   SET played_at = COALESCE(m.played_at, now()),
		       status = CASE WHEN m.status = 'new' THEN 'saved' ELSE m.status END
		  FROM voicemail_boxes b
		 WHERE m.id = $1 AND b.id = m.box_id AND b.tenant_id = $2 AND m.status <> 'deleted'`
	tag, err := s.DB.Exec(ctx, q, msgID, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrVoicemailMessageNotFound
	}
	return nil
}

// DeleteVoicemailMessageForTenant soft-deletes a message (status='deleted',
// deleted_at=now). The audio file on the FS host is left for FS/cleanup to
// reap; we just stop surfacing it.
func (s *Store) DeleteVoicemailMessageForTenant(ctx context.Context, tenantID, msgID uuid.UUID) error {
	const q = `
		UPDATE voicemail_messages m
		   SET status = 'deleted', deleted_at = now()
		  FROM voicemail_boxes b
		 WHERE m.id = $1 AND b.id = m.box_id AND b.tenant_id = $2 AND m.status <> 'deleted'`
	tag, err := s.DB.Exec(ctx, q, msgID, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrVoicemailMessageNotFound
	}
	return nil
}

// PendingTranscriptMessage is a voicemail message awaiting transcription: it
// has audio on disk and no transcript yet.
type PendingTranscriptMessage struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	AudioPath string
}

// ListVoicemailMessagesPendingTranscript returns non-deleted messages that have
// an audio file and no transcript yet, newest first. NULL-safe: the WHERE
// guarantees audio_path is non-empty, and only the three needed columns are
// selected (transcript itself is never scanned here — we just test IS NULL).
func (s *Store) ListVoicemailMessagesPendingTranscript(ctx context.Context, limit int) ([]PendingTranscriptMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	const q = `
		SELECT m.id, b.tenant_id, m.audio_path
		  FROM voicemail_messages m
		  JOIN voicemail_boxes b ON b.id = m.box_id
		 WHERE m.transcript IS NULL
		   AND m.audio_path IS NOT NULL AND m.audio_path <> ''
		   AND m.status <> 'deleted'
		 ORDER BY m.received_at DESC
		 LIMIT $1`
	rows, err := s.DB.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingTranscriptMessage
	for rows.Next() {
		var p PendingTranscriptMessage
		if err := rows.Scan(&p.ID, &p.TenantID, &p.AudioPath); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetVoicemailTranscript stores the transcript for one message. NULLIF maps an
// empty transcript back to NULL so the pending query won't re-skip it forever
// (it'll retry next tick).
func (s *Store) SetVoicemailTranscript(ctx context.Context, msgID uuid.UUID, transcript string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE voicemail_messages SET transcript = NULLIF($2,'') WHERE id = $1`,
		msgID, transcript)
	return err
}

// GetVoicemailTranscript returns the stored transcript for a message (tenant-
// scoped), or "" when none. Read via this dedicated getter so the main
// VoicemailMessage scan paths stay untouched.
func (s *Store) GetVoicemailTranscript(ctx context.Context, tenantID, msgID uuid.UUID) (string, error) {
	const q = `
		SELECT COALESCE(m.transcript,'')
		  FROM voicemail_messages m
		  JOIN voicemail_boxes b ON b.id = m.box_id
		 WHERE m.id = $1 AND b.tenant_id = $2`
	var transcript string
	err := s.DB.QueryRow(ctx, q, msgID, tenantID).Scan(&transcript)
	if err != nil {
		return "", err
	}
	return transcript, nil
}

// ListVoicemailTranscriptsForBox returns a map of message id → transcript for
// the non-empty transcripts in a box. The inbox view uses it to annotate the
// message list without changing the existing ListVoicemailMessagesForBox scan.
func (s *Store) ListVoicemailTranscriptsForBox(ctx context.Context, boxID uuid.UUID) (map[uuid.UUID]string, error) {
	const q = `
		SELECT id, transcript
		  FROM voicemail_messages
		 WHERE box_id = $1 AND transcript IS NOT NULL AND transcript <> ''
		   AND status <> 'deleted'`
	rows, err := s.DB.Query(ctx, q, boxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]string{}
	for rows.Next() {
		var id uuid.UUID
		var transcript string
		if err := rows.Scan(&id, &transcript); err != nil {
			return nil, err
		}
		out[id] = transcript
	}
	return out, rows.Err()
}

func (s *Store) GetVoicemailBoxByExtensionID(ctx context.Context, extID uuid.UUID) (*VoicemailBox, error) {
	const q = `
		SELECT id, tenant_id, extension_id, pin, COALESCE(email::text,''),
		       timezone, max_messages, max_msg_duration_sec,
		       COALESCE(greeting_path,''), enabled,
		       voicemail_email_enabled, COALESCE(voicemail_email_address::text,''),
		       created_at, updated_at
		  FROM voicemail_boxes WHERE extension_id = $1`
	var b VoicemailBox
	err := s.DB.QueryRow(ctx, q, extID).Scan(
		&b.ID, &b.TenantID, &b.ExtensionID, &b.PIN, &b.Email,
		&b.Timezone, &b.MaxMessages, &b.MaxMsgDurationSec,
		&b.GreetingPath, &b.Enabled,
		&b.EmailEnabled, &b.EmailAddress,
		&b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// ErrVoicemailBoxNotFound is returned when a box-scoped update misses.
var ErrVoicemailBoxNotFound = errors.New("voicemail box not found for this extension")

// UpdateVoicemailEmailNotify sets the VM-to-email opt-in (enabled flag +
// recipient) for the box belonging to extID. An empty address forces enabled
// to false so we never persist "enabled with nowhere to send". The address is
// stored via NULLIF so an empty string becomes SQL NULL. Returns
// ErrVoicemailBoxNotFound when the extension has no box. Used by both the admin
// extension page and the owner self-service page.
func (s *Store) UpdateVoicemailEmailNotify(ctx context.Context, extID uuid.UUID, enabled bool, address string) error {
	if address == "" {
		enabled = false
	}
	const q = `
		UPDATE voicemail_boxes
		   SET voicemail_email_enabled = $2,
		       voicemail_email_address = NULLIF($3,'')::citext
		 WHERE extension_id = $1`
	tag, err := s.DB.Exec(ctx, q, extID, enabled, address)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrVoicemailBoxNotFound
	}
	return nil
}

// DIDVoicemailTarget is what the inbound PSTN handler needs to deliver a
// call straight to voicemail (destination_kind = 'voicemail').
type DIDVoicemailTarget struct {
	BoxID       uuid.UUID
	TenantID    uuid.UUID
	SIPUsername string
	SIPDomain   string
}

func (s *Store) LookupDIDVoicemailTarget(ctx context.Context, e164 string) (*DIDVoicemailTarget, error) {
	const q = `
		SELECT vb.id, vb.tenant_id, e.sip_username, sd.domain
		  FROM dids d
		  JOIN voicemail_boxes vb ON vb.id = d.destination_id
		                          AND d.destination_kind = 'voicemail'
		  JOIN extensions    e   ON e.id  = vb.extension_id
		  JOIN sip_domains   sd  ON sd.id = e.sip_domain_id
		 WHERE d.e164 = $1 AND d.enabled = true
		   AND vb.enabled = true AND e.status = 'active'
		 LIMIT 1`
	var t DIDVoicemailTarget
	err := s.DB.QueryRow(ctx, q, e164).Scan(
		&t.BoxID, &t.TenantID, &t.SIPUsername, &t.SIPDomain,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// DirectoryUser is everything mod_voicemail's directory lookup needs about
// one SIP user: identity + voicemail box (optional).
type DirectoryUser struct {
	Extension    Extension
	Domain       string
	VoicemailBox *VoicemailBox // nil if user has no enabled box
}

// LookupDirectoryUser resolves (domain, user) to extension + optional VM box
// in one trip. Used by the FreeSWITCH directory XML handler.
func (s *Store) LookupDirectoryUser(ctx context.Context, domain, user string) (*DirectoryUser, error) {
	// vb.* columns flow through a LEFT JOIN — they're all NULL when the
	// extension has no voicemail box. COALESCE every vb.* scalar so the
	// scanner never sees a NULL going into a non-pointer Go field. vb.id
	// stays NULLABLE (scanned into *uuid.UUID) so we can distinguish "no
	// box" from "box exists".
	const q = `
		SELECT e.id, e.tenant_id, e.sip_domain_id, e.extension, e.sip_username,
		       '', e.user_id, COALESCE(e.display_name,''),
		       e.voicemail_enabled, e.status, e.created_at, e.updated_at,
		       vb.id,
		       COALESCE(vb.pin,''),
		       COALESCE(vb.email::text,''),
		       COALESCE(vb.timezone,''),
		       COALESCE(vb.max_messages,0),
		       COALESCE(vb.max_msg_duration_sec,0),
		       COALESCE(vb.greeting_path,''),
		       COALESCE(vb.enabled,false)
		  FROM extensions e
		  JOIN sip_domains sd ON sd.id = e.sip_domain_id
		  LEFT JOIN voicemail_boxes vb
		    ON vb.extension_id = e.id AND vb.enabled = true
		 WHERE sd.domain = $1 AND e.sip_username = $2 AND e.status = 'active'
		 LIMIT 1`
	var (
		ext    Extension
		vb     VoicemailBox
		hasBox bool
	)
	var (
		vbID *uuid.UUID
	)
	err := s.DB.QueryRow(ctx, q, domain, user).Scan(
		&ext.ID, &ext.TenantID, &ext.SIPDomainID, &ext.Extension, &ext.SIPUsername,
		&ext.SIPPassword, &ext.UserID, &ext.DisplayName,
		&ext.VoicemailEnabled, &ext.Status, &ext.CreatedAt, &ext.UpdatedAt,
		&vbID, &vb.PIN, &vb.Email, &vb.Timezone,
		&vb.MaxMessages, &vb.MaxMsgDurationSec,
		&vb.GreetingPath, &vb.Enabled,
	)
	if err != nil {
		return nil, err
	}
	out := &DirectoryUser{Extension: ext, Domain: domain}
	if vbID != nil {
		vb.ID = *vbID
		vb.TenantID = ext.TenantID
		vb.ExtensionID = ext.ID
		hasBox = true
	}
	if hasBox {
		out.VoicemailBox = &vb
	}
	return out, nil
}
