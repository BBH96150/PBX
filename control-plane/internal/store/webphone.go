package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"

	"github.com/google/uuid"
)

// IssuedWebphoneCreds is the one-time bundle the portal hands to the browser
// at /admin/softphone load. Plaintext password is NEVER persisted — only HA1.
type IssuedWebphoneCreds struct {
	ExtensionID uuid.UUID `json:"extension_id"`
	Extension   string    `json:"extension"`
	Username    string    `json:"username"`   // <sip_username>-wp
	Password    string    `json:"password"`   // plaintext, shown once
	Domain      string    `json:"domain"`     // primary SIP domain
	DisplayName string    `json:"display_name"`
}

// IssueWebphoneCredentials rotates the webphone password for an extension
// belonging to userID. Marks webphone_enabled, generates a fresh password,
// computes HA1/HA1b against the extension's primary domain, returns plaintext.
//
// userID is enforced — only the owner of the extension (or a super-admin
// path that bypasses this layer) can rotate credentials.
func (s *Store) IssueWebphoneCredentials(ctx context.Context, userID, extensionID uuid.UUID) (*IssuedWebphoneCreds, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1. Resolve extension + domain + ownership.
	var (
		sipUsername string
		extension   string
		displayName string
		extOwner    *uuid.UUID
		domain      string
	)
	if err := tx.QueryRow(ctx, `
		SELECT e.sip_username, e.extension, COALESCE(e.display_name,''), e.user_id, d.domain
		  FROM extensions e
		  JOIN sip_domains d ON d.id = e.sip_domain_id
		 WHERE e.id = $1 AND e.status = 'active'`,
		extensionID,
	).Scan(&sipUsername, &extension, &displayName, &extOwner, &domain); err != nil {
		return nil, errors.New("extension not found")
	}
	if extOwner == nil || *extOwner != userID {
		return nil, errors.New("you do not own that extension")
	}

	// 2. Generate fresh password + HA1.
	password := newWebphonePassword()
	webUsername := sipUsername + "-wp"
	ha1 := ComputeHA1(webUsername, domain, password)
	ha1b := ComputeHA1b(webUsername, domain, password)

	if _, err := tx.Exec(ctx, `
		UPDATE extensions
		   SET webphone_enabled    = true,
		       webphone_username   = $2,
		       webphone_ha1        = $3,
		       webphone_ha1b       = $4,
		       webphone_rotated_at = now()
		 WHERE id = $1`,
		extensionID, webUsername, ha1, ha1b,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &IssuedWebphoneCreds{
		ExtensionID: extensionID,
		Extension:   extension,
		Username:    webUsername,
		Password:    password,
		Domain:      domain,
		DisplayName: displayName,
	}, nil
}

// FindOwnedExtensions returns extensions where user_id = userID. Used to
// resolve "which extension does this portal user own" for the softphone.
func (s *Store) FindOwnedExtensions(ctx context.Context, userID uuid.UUID) ([]Extension, error) {
	const q = `
		SELECT e.id, e.tenant_id, e.sip_domain_id, e.extension, e.sip_username,
		       '', e.user_id, COALESCE(e.display_name,''),
		       e.voicemail_enabled,
		       e.do_not_disturb, COALESCE(e.cf_immediate,''), COALESCE(e.cf_busy,''),
		       COALESCE(e.cf_no_answer,''), e.recording_enabled,
		       e.status, e.created_at, e.updated_at
		  FROM extensions e
		 WHERE e.user_id = $1 AND e.status = 'active'
		 ORDER BY e.extension`
	rows, err := s.DB.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Extension
	for rows.Next() {
		var e Extension
		if err := rows.Scan(
			&e.ID, &e.TenantID, &e.SIPDomainID, &e.Extension, &e.SIPUsername,
			&e.SIPPassword, &e.UserID, &e.DisplayName,
			&e.VoicemailEnabled,
			&e.DoNotDisturb, &e.CFImmediate, &e.CFBusy, &e.CFNoAnswer, &e.RecordingEnabled,
			&e.Status, &e.CreatedAt, &e.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// newWebphonePassword returns a high-entropy URL-safe password. Long enough
// that it survives base64 round-trips in SIP digest without collisions.
func newWebphonePassword() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
