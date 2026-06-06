package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrExtensionNotFound is returned when an extension update/delete misses.
var ErrExtensionNotFound = errors.New("extension not found")

type Extension struct {
	ID               uuid.UUID  `json:"id"`
	TenantID         uuid.UUID  `json:"tenant_id"`
	SIPDomainID      uuid.UUID  `json:"sip_domain_id"`
	Extension        string     `json:"extension"`
	SIPUsername      string     `json:"sip_username"`
	SIPPassword      string     `json:"sip_password,omitempty"` // returned on create only
	UserID           *uuid.UUID `json:"user_id,omitempty"`
	DisplayName      string     `json:"display_name,omitempty"`
	VoicemailEnabled bool       `json:"voicemail_enabled"`
	// Phase 3 Wave 5.0:
	DoNotDisturb     bool   `json:"do_not_disturb"`
	CFImmediate      string `json:"cf_immediate,omitempty"`
	CFBusy           string `json:"cf_busy,omitempty"`
	CFNoAnswer       string `json:"cf_no_answer,omitempty"`
	RecordingEnabled bool   `json:"recording_enabled"`

	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateExtension creates an extension with auto-generated SIP password if blank,
// and precomputes ha1/ha1b for Kamailio.
func (s *Store) CreateExtension(ctx context.Context, tenantID, sipDomainID uuid.UUID, ext, sipUser, sipPass, displayName string) (*Extension, error) {
	domain, err := s.getDomainByID(ctx, sipDomainID)
	if err != nil {
		return nil, err
	}
	if sipUser == "" {
		sipUser = ext
	}
	if sipPass == "" {
		sipPass = randomToken(12)
	}
	ha1 := ComputeHA1(sipUser, domain, sipPass)
	ha1b := ComputeHA1b(sipUser, domain, sipPass)

	const q = `
		INSERT INTO extensions
		    (tenant_id, sip_domain_id, extension, sip_username, sip_password,
		     ha1, ha1b, display_name)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''))
		RETURNING id, tenant_id, sip_domain_id, extension, sip_username,
		          sip_password, user_id, COALESCE(display_name, ''),
		          voicemail_enabled,
		          do_not_disturb, COALESCE(cf_immediate,''), COALESCE(cf_busy,''),
		          COALESCE(cf_no_answer,''), recording_enabled,
		          status, created_at, updated_at`
	var e Extension
	err = s.DB.QueryRow(ctx, q,
		tenantID, sipDomainID, ext, sipUser, sipPass, ha1, ha1b, displayName,
	).Scan(
		&e.ID, &e.TenantID, &e.SIPDomainID, &e.Extension, &e.SIPUsername,
		&e.SIPPassword, &e.UserID, &e.DisplayName,
		&e.VoicemailEnabled,
		&e.DoNotDisturb, &e.CFImmediate, &e.CFBusy, &e.CFNoAnswer, &e.RecordingEnabled,
		&e.Status, &e.CreatedAt, &e.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// UpdateExtensionFeaturesInput uses pointers so callers can specify which
// fields to update — nil leaves the field unchanged. This is the equivalent
// of PATCH semantics for the Phase 3 Wave 5.0 feature set.
type UpdateExtensionFeaturesInput struct {
	DoNotDisturb     *bool
	CFImmediate      *string
	CFBusy           *string
	CFNoAnswer       *string
	VoicemailEnabled *bool
	RecordingEnabled *bool
}

func (s *Store) UpdateExtensionFeatures(ctx context.Context, id uuid.UUID, in UpdateExtensionFeaturesInput) (*Extension, error) {
	const q = `
		UPDATE extensions
		   SET do_not_disturb    = COALESCE($2, do_not_disturb),
		       cf_immediate      = COALESCE($3, cf_immediate),
		       cf_busy           = COALESCE($4, cf_busy),
		       cf_no_answer      = COALESCE($5, cf_no_answer),
		       voicemail_enabled = COALESCE($6, voicemail_enabled),
		       recording_enabled = COALESCE($7, recording_enabled)
		 WHERE id = $1
	 RETURNING id, tenant_id, sip_domain_id, extension, sip_username,
		          '', user_id, COALESCE(display_name, ''),
		          voicemail_enabled,
		          do_not_disturb, COALESCE(cf_immediate,''), COALESCE(cf_busy,''),
		          COALESCE(cf_no_answer,''), recording_enabled,
		          status, created_at, updated_at`
	var e Extension
	err := s.DB.QueryRow(ctx, q,
		id, in.DoNotDisturb, in.CFImmediate, in.CFBusy, in.CFNoAnswer,
		in.VoicemailEnabled, in.RecordingEnabled,
	).Scan(
		&e.ID, &e.TenantID, &e.SIPDomainID, &e.Extension, &e.SIPUsername,
		&e.SIPPassword, &e.UserID, &e.DisplayName,
		&e.VoicemailEnabled,
		&e.DoNotDisturb, &e.CFImmediate, &e.CFBusy, &e.CFNoAnswer, &e.RecordingEnabled,
		&e.Status, &e.CreatedAt, &e.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// UpdateExtensionDisplayName changes only the human label. The extension
// number / SIP identity stay fixed (changing those would break registration).
func (s *Store) UpdateExtensionDisplayName(ctx context.Context, id uuid.UUID, displayName string) error {
	tag, err := s.DB.Exec(ctx,
		`UPDATE extensions SET display_name = NULLIF($2,''), updated_at = now() WHERE id = $1`,
		id, displayName)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrExtensionNotFound
	}
	return nil
}

// SetExtensionUser assigns (or, with nil, clears) the owning user of an
// extension — the link that powers the self-service portal.
func (s *Store) SetExtensionUser(ctx context.Context, extID uuid.UUID, userID *uuid.UUID) error {
	tag, err := s.DB.Exec(ctx,
		`UPDATE extensions SET user_id = $2, updated_at = now() WHERE id = $1`,
		extID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrExtensionNotFound
	}
	return nil
}

// ExtensionInboundDIDs returns the E.164 numbers of any DIDs that route
// directly to this extension. Used to block deletion until they're reassigned
// (dids.destination_id is not an FK, so a delete would silently orphan them).
func (s *Store) ExtensionInboundDIDs(ctx context.Context, extID uuid.UUID) ([]string, error) {
	const q = `SELECT e164 FROM dids WHERE destination_kind = 'extension' AND destination_id = $1 ORDER BY e164`
	rows, err := s.DB.Query(ctx, q, extID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// DeleteExtension hard-deletes an extension. Members / agents / device lines /
// voicemail boxes are removed by ON DELETE CASCADE. Callers must first confirm
// no DID points at it (see ExtensionInboundDIDs).
func (s *Store) DeleteExtension(ctx context.Context, id uuid.UUID) error {
	tag, err := s.DB.Exec(ctx, `DELETE FROM extensions WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrExtensionNotFound
	}
	return nil
}

// LookupExtensionForRouting resolves (tenant_domain, extension_number) → extension+domain
// for the FreeSWITCH dialplan handler.
func (s *Store) LookupExtensionForRouting(ctx context.Context, domain, extNum string) (*Extension, string, error) {
	const q = `
		SELECT e.id, e.tenant_id, e.sip_domain_id, e.extension, e.sip_username,
		       '', e.user_id, COALESCE(e.display_name, ''),
		       e.voicemail_enabled,
		       e.do_not_disturb, COALESCE(e.cf_immediate,''), COALESCE(e.cf_busy,''),
		       COALESCE(e.cf_no_answer,''), e.recording_enabled,
		       e.status, e.created_at, e.updated_at,
		       d.domain
		  FROM extensions e
		  JOIN sip_domains d ON d.id = e.sip_domain_id
		 WHERE d.domain = $1 AND e.extension = $2 AND e.status = 'active'
		 LIMIT 1`
	var e Extension
	var dom string
	err := s.DB.QueryRow(ctx, q, domain, extNum).Scan(
		&e.ID, &e.TenantID, &e.SIPDomainID, &e.Extension, &e.SIPUsername,
		&e.SIPPassword, &e.UserID, &e.DisplayName,
		&e.VoicemailEnabled,
		&e.DoNotDisturb, &e.CFImmediate, &e.CFBusy, &e.CFNoAnswer, &e.RecordingEnabled,
		&e.Status, &e.CreatedAt, &e.UpdatedAt, &dom,
	)
	if err != nil {
		return nil, "", err
	}
	return &e, dom, nil
}

// LookupExtensionByNumberOnly (Phase 1 fallback when tenant domain not stamped).
func (s *Store) LookupExtensionByNumberOnly(ctx context.Context, extNum string) (*Extension, string, error) {
	const q = `
		SELECT e.id, e.tenant_id, e.sip_domain_id, e.extension, e.sip_username,
		       '', e.user_id, COALESCE(e.display_name, ''),
		       e.voicemail_enabled,
		       e.do_not_disturb, COALESCE(e.cf_immediate,''), COALESCE(e.cf_busy,''),
		       COALESCE(e.cf_no_answer,''), e.recording_enabled,
		       e.status, e.created_at, e.updated_at,
		       d.domain
		  FROM extensions e
		  JOIN sip_domains d ON d.id = e.sip_domain_id
		 WHERE e.extension = $1 AND e.status = 'active'
		 ORDER BY e.created_at LIMIT 1`
	var e Extension
	var dom string
	err := s.DB.QueryRow(ctx, q, extNum).Scan(
		&e.ID, &e.TenantID, &e.SIPDomainID, &e.Extension, &e.SIPUsername,
		&e.SIPPassword, &e.UserID, &e.DisplayName,
		&e.VoicemailEnabled,
		&e.DoNotDisturb, &e.CFImmediate, &e.CFBusy, &e.CFNoAnswer, &e.RecordingEnabled,
		&e.Status, &e.CreatedAt, &e.UpdatedAt, &dom,
	)
	if err != nil {
		return nil, "", err
	}
	return &e, dom, nil
}

func (s *Store) getDomainByID(ctx context.Context, id uuid.UUID) (string, error) {
	var d string
	err := s.DB.QueryRow(ctx, `SELECT domain FROM sip_domains WHERE id = $1`, id).Scan(&d)
	return d, err
}

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
