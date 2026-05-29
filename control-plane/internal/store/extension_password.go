package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/google/uuid"
)

// GetExtensionSIPPassword returns the plaintext password + the primary SIP
// domain for an extension. Used by the portal to display SIP credentials.
func (s *Store) GetExtensionSIPPassword(ctx context.Context, id uuid.UUID) (sipUsername, sipPassword, domain string, err error) {
	const q = `
		SELECT e.sip_username, e.sip_password, d.domain
		  FROM extensions e
		  JOIN sip_domains d ON d.id = e.sip_domain_id
		 WHERE e.id = $1`
	err = s.DB.QueryRow(ctx, q, id).Scan(&sipUsername, &sipPassword, &domain)
	return
}

// RotateExtensionSIPPassword generates a fresh password, recomputes HA1/HA1b,
// returns the new plaintext (caller displays once). Existing registrations
// drop on the next SIP digest challenge.
func (s *Store) RotateExtensionSIPPassword(ctx context.Context, id uuid.UUID) (newPassword string, err error) {
	var sipUsername, domain string
	if err = s.DB.QueryRow(ctx, `
		SELECT e.sip_username, d.domain
		  FROM extensions e
		  JOIN sip_domains d ON d.id = e.sip_domain_id
		 WHERE e.id = $1`, id,
	).Scan(&sipUsername, &domain); err != nil {
		return "", err
	}
	b := make([]byte, 12)
	if _, err = rand.Read(b); err != nil {
		return "", err
	}
	newPassword = hex.EncodeToString(b)
	newHA1 := ComputeHA1(sipUsername, domain, newPassword)
	newHA1b := ComputeHA1b(sipUsername, domain, newPassword)
	tag, err := s.DB.Exec(ctx, `
		UPDATE extensions
		   SET sip_password = $2, ha1 = $3, ha1b = $4, updated_at = now()
		 WHERE id = $1`, id, newPassword, newHA1, newHA1b)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", errors.New("extension not found")
	}
	return newPassword, nil
}
