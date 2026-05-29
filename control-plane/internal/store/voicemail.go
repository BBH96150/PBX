package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type VoicemailBox struct {
	ID                 uuid.UUID `json:"id"`
	TenantID           uuid.UUID `json:"tenant_id"`
	ExtensionID        uuid.UUID `json:"extension_id"`
	PIN                string    `json:"pin,omitempty"`
	Email              string    `json:"email,omitempty"`
	Timezone           string    `json:"timezone"`
	MaxMessages        int       `json:"max_messages"`
	MaxMsgDurationSec  int       `json:"max_msg_duration_sec"`
	GreetingPath       string    `json:"greeting_path,omitempty"`
	Enabled            bool      `json:"enabled"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type CreateVoicemailBoxInput struct {
	TenantID            uuid.UUID
	ExtensionID         uuid.UUID
	PIN                 string
	Email               string
	Timezone            string
	MaxMessages         int
	MaxMsgDurationSec   int
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
		          COALESCE(greeting_path,''), enabled, created_at, updated_at`
	var b VoicemailBox
	err := s.DB.QueryRow(ctx, q,
		in.TenantID, in.ExtensionID, in.PIN, in.Email, in.Timezone,
		in.MaxMessages, in.MaxMsgDurationSec,
	).Scan(
		&b.ID, &b.TenantID, &b.ExtensionID, &b.PIN, &b.Email,
		&b.Timezone, &b.MaxMessages, &b.MaxMsgDurationSec,
		&b.GreetingPath, &b.Enabled, &b.CreatedAt, &b.UpdatedAt,
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
		       COALESCE(vb.greeting_path,''), vb.enabled, vb.created_at, vb.updated_at
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
		&b.GreetingPath, &b.Enabled, &b.CreatedAt, &b.UpdatedAt,
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

func (s *Store) GetVoicemailBoxByExtensionID(ctx context.Context, extID uuid.UUID) (*VoicemailBox, error) {
	const q = `
		SELECT id, tenant_id, extension_id, pin, COALESCE(email::text,''),
		       timezone, max_messages, max_msg_duration_sec,
		       COALESCE(greeting_path,''), enabled, created_at, updated_at
		  FROM voicemail_boxes WHERE extension_id = $1`
	var b VoicemailBox
	err := s.DB.QueryRow(ctx, q, extID).Scan(
		&b.ID, &b.TenantID, &b.ExtensionID, &b.PIN, &b.Email,
		&b.Timezone, &b.MaxMessages, &b.MaxMsgDurationSec,
		&b.GreetingPath, &b.Enabled, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &b, nil
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
		ext Extension
		vb  VoicemailBox
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
