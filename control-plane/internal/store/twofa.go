package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// ---------------------------------------------------------------------------
// 2FA methods (currently only TOTP is wired; schema is extensible).
// ---------------------------------------------------------------------------

type TwoFAMethod struct {
	ID                uuid.UUID  `json:"id"`
	UserID            uuid.UUID  `json:"user_id"`
	Kind              string     `json:"kind"`
	SecretCiphertext  []byte     `json:"-"`
	SecretNonce       []byte     `json:"-"`
	Label             string     `json:"label"`
	ConfirmedAt       *time.Time `json:"confirmed_at,omitempty"`
	LastUsedAt        *time.Time `json:"last_used_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

type CreateTwoFAMethodInput struct {
	UserID           uuid.UUID
	Kind             string // "totp"
	SecretCiphertext []byte
	SecretNonce      []byte
	Label            string
}

// CreatePendingTwoFAMethod inserts an unconfirmed method. The caller calls
// ConfirmTwoFAMethod after the first valid code is supplied.
func (s *Store) CreatePendingTwoFAMethod(ctx context.Context, in CreateTwoFAMethodInput) (*TwoFAMethod, error) {
	if in.Kind == "" {
		in.Kind = "totp"
	}
	const q = `
		INSERT INTO user_2fa_methods (user_id, kind, secret_ciphertext, secret_nonce, label)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (user_id, kind, label) DO UPDATE
		   SET secret_ciphertext = EXCLUDED.secret_ciphertext,
		       secret_nonce      = EXCLUDED.secret_nonce,
		       confirmed_at      = NULL
		RETURNING id, user_id, kind, secret_ciphertext, secret_nonce,
		          label, confirmed_at, last_used_at, created_at`
	var m TwoFAMethod
	err := s.DB.QueryRow(ctx, q,
		in.UserID, in.Kind, in.SecretCiphertext, in.SecretNonce, in.Label,
	).Scan(
		&m.ID, &m.UserID, &m.Kind, &m.SecretCiphertext, &m.SecretNonce,
		&m.Label, &m.ConfirmedAt, &m.LastUsedAt, &m.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ConfirmTwoFAMethod stamps confirmed_at and last_used_at.
func (s *Store) ConfirmTwoFAMethod(ctx context.Context, id uuid.UUID) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE user_2fa_methods SET confirmed_at = now(), last_used_at = now() WHERE id = $1`, id)
	return err
}

// MarkTwoFAMethodUsed bumps last_used_at after a successful login challenge.
func (s *Store) MarkTwoFAMethodUsed(ctx context.Context, id uuid.UUID) error {
	_, err := s.DB.Exec(ctx, `UPDATE user_2fa_methods SET last_used_at = now() WHERE id = $1`, id)
	return err
}

// ListConfirmedTwoFAMethods is the login-time check: any confirmed method?
func (s *Store) ListConfirmedTwoFAMethods(ctx context.Context, userID uuid.UUID) ([]TwoFAMethod, error) {
	const q = `
		SELECT id, user_id, kind, secret_ciphertext, secret_nonce,
		       label, confirmed_at, last_used_at, created_at
		  FROM user_2fa_methods
		 WHERE user_id = $1 AND confirmed_at IS NOT NULL
		 ORDER BY created_at`
	rows, err := s.DB.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TwoFAMethod
	for rows.Next() {
		var m TwoFAMethod
		if err := rows.Scan(
			&m.ID, &m.UserID, &m.Kind, &m.SecretCiphertext, &m.SecretNonce,
			&m.Label, &m.ConfirmedAt, &m.LastUsedAt, &m.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CountConfirmedTwoFAMethods is a cheap "does this user have any 2FA?" check.
func (s *Store) CountConfirmedTwoFAMethods(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := s.DB.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_2fa_methods WHERE user_id = $1 AND confirmed_at IS NOT NULL`,
		userID,
	).Scan(&n)
	return n, err
}

// HasAnyEnrolledTwoFA returns true if there are any rows in user_2fa_methods,
// confirmed or not. Used by startup to gate the missing-key check.
func (s *Store) HasAnyEnrolledTwoFA(ctx context.Context) (bool, error) {
	var n int
	err := s.DB.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_2fa_methods WHERE confirmed_at IS NOT NULL`).Scan(&n)
	return n > 0, err
}

// ResetTwoFAForUser drops every 2FA method + recovery code + trusted device
// for a user. Atomic. Used by admin-reset and by user-self-disable.
func (s *Store) ResetTwoFAForUser(ctx context.Context, userID uuid.UUID) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM user_2fa_methods WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_2fa_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_trusted_devices WHERE user_id = $1`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ---------------------------------------------------------------------------
// Recovery codes — 10 single-use bcrypt-hashed codes per enrollment.
// ---------------------------------------------------------------------------

// IssuedRecoveryCodes returns the plaintext set shown to the user exactly
// once at enrollment time. Persisted as bcrypt hashes.
type IssuedRecoveryCodes struct {
	UserID    uuid.UUID
	Plaintext []string
}

// GenerateRecoveryCodes wipes any prior unused codes and inserts a fresh batch.
// Returns the plaintext set for one-time display. Run in a transaction so
// either all codes land or none do.
func (s *Store) GenerateRecoveryCodes(ctx context.Context, userID uuid.UUID) (*IssuedRecoveryCodes, error) {
	plain := make([]string, 10)
	for i := range plain {
		plain[i] = newRecoveryCode()
	}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`DELETE FROM user_2fa_recovery_codes WHERE user_id = $1 AND used_at IS NULL`,
		userID); err != nil {
		return nil, err
	}
	for _, p := range plain {
		h, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO user_2fa_recovery_codes (user_id, code_hash) VALUES ($1, $2)`,
			userID, string(h)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &IssuedRecoveryCodes{UserID: userID, Plaintext: plain}, nil
}

// CountUnusedRecoveryCodes is shown in the UI banner after a code is used.
func (s *Store) CountUnusedRecoveryCodes(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := s.DB.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_2fa_recovery_codes WHERE user_id = $1 AND used_at IS NULL`,
		userID,
	).Scan(&n)
	return n, err
}

// ConsumeRecoveryCode tries to match `plaintext` against any unused code for
// the user. On match: marks it used. bcrypt-compares every row — there are
// only ever ≤10 so this is fine.
func (s *Store) ConsumeRecoveryCode(ctx context.Context, userID uuid.UUID, plaintext string) error {
	// Codes are stored bcrypted in their displayed form ("xxxx-yyyy"). Be
	// forgiving on input: accept the user pasting either with or without
	// dashes by trying both forms against each hash.
	in := strings.TrimSpace(plaintext)
	if in == "" {
		return ErrInvalidRecoveryCode
	}
	candidates := []string{in}
	stripped := strings.ReplaceAll(in, "-", "")
	if stripped != in {
		candidates = append(candidates, stripped)
	}
	if len(stripped) == 8 {
		candidates = append(candidates, stripped[:4]+"-"+stripped[4:])
	}

	rows, err := s.DB.Query(ctx,
		`SELECT id, code_hash FROM user_2fa_recovery_codes WHERE user_id = $1 AND used_at IS NULL`,
		userID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var hash string
		if err := rows.Scan(&id, &hash); err != nil {
			return err
		}
		for _, c := range candidates {
			if bcrypt.CompareHashAndPassword([]byte(hash), []byte(c)) == nil {
				_, err := s.DB.Exec(ctx, `UPDATE user_2fa_recovery_codes SET used_at = now() WHERE id = $1`, id)
				return err
			}
		}
	}
	return ErrInvalidRecoveryCode
}

// ---------------------------------------------------------------------------
// Trusted devices.
// ---------------------------------------------------------------------------

type TrustedDevice struct {
	ID          uuid.UUID  `json:"id"`
	UserID      uuid.UUID  `json:"user_id"`
	TokenPrefix string     `json:"token_prefix"`
	Label       string     `json:"label"`
	IPAddress   string     `json:"ip_address,omitempty"`
	ExpiresAt   time.Time  `json:"expires_at"`
	LastSeenAt  *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
}

type IssuedTrustedDevice struct {
	TrustedDevice
	Plaintext string `json:"token"`
}

// CreateTrustedDevice mints a 30-day device cookie. The plaintext goes in
// the cookie and is never persisted — only the bcrypt hash + 8-char prefix.
func (s *Store) CreateTrustedDevice(ctx context.Context, userID uuid.UUID, label, ip string) (*IssuedTrustedDevice, error) {
	plaintext, prefix := generateTrustToken()
	h, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	const q = `
		INSERT INTO user_trusted_devices
		    (user_id, token_prefix, token_hash, label, ip_address)
		VALUES ($1,$2,$3,$4,NULLIF($5,'')::inet)
		RETURNING id, user_id, token_prefix, label, COALESCE(host(ip_address),''),
		          expires_at, last_seen_at, created_at, revoked_at`
	var d TrustedDevice
	err = s.DB.QueryRow(ctx, q, userID, prefix, string(h), label, ip).Scan(
		&d.ID, &d.UserID, &d.TokenPrefix, &d.Label, &d.IPAddress,
		&d.ExpiresAt, &d.LastSeenAt, &d.CreatedAt, &d.RevokedAt,
	)
	if err != nil {
		return nil, err
	}
	return &IssuedTrustedDevice{TrustedDevice: d, Plaintext: plaintext}, nil
}

// VerifyTrustedDevice extracts the prefix, bcrypt-checks every candidate,
// returns the row on match. Returns ErrInvalidTrustToken on no-match, expired,
// or revoked.
func (s *Store) VerifyTrustedDevice(ctx context.Context, userID uuid.UUID, plaintext string) (*TrustedDevice, error) {
	prefix, ok := extractTrustPrefix(plaintext)
	if !ok {
		return nil, ErrInvalidTrustToken
	}
	const q = `
		SELECT id, user_id, token_prefix, token_hash, label,
		       COALESCE(host(ip_address),''),
		       expires_at, last_seen_at, created_at, revoked_at
		  FROM user_trusted_devices
		 WHERE user_id = $1 AND token_prefix = $2`
	rows, err := s.DB.Query(ctx, q, userID, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d TrustedDevice
		var hash string
		if err := rows.Scan(
			&d.ID, &d.UserID, &d.TokenPrefix, &hash, &d.Label, &d.IPAddress,
			&d.ExpiresAt, &d.LastSeenAt, &d.CreatedAt, &d.RevokedAt,
		); err != nil {
			return nil, err
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) != nil {
			continue
		}
		if d.RevokedAt != nil || d.ExpiresAt.Before(time.Now()) {
			return nil, ErrInvalidTrustToken
		}
		return &d, nil
	}
	return nil, ErrInvalidTrustToken
}

func (s *Store) MarkTrustedDeviceSeen(ctx context.Context, id uuid.UUID) error {
	_, err := s.DB.Exec(ctx, `UPDATE user_trusted_devices SET last_seen_at = now() WHERE id = $1`, id)
	return err
}

func (s *Store) ListTrustedDevices(ctx context.Context, userID uuid.UUID) ([]TrustedDevice, error) {
	const q = `
		SELECT id, user_id, token_prefix, label, COALESCE(host(ip_address),''),
		       expires_at, last_seen_at, created_at, revoked_at
		  FROM user_trusted_devices
		 WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now()
		 ORDER BY created_at DESC`
	rows, err := s.DB.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrustedDevice
	for rows.Next() {
		var d TrustedDevice
		if err := rows.Scan(
			&d.ID, &d.UserID, &d.TokenPrefix, &d.Label, &d.IPAddress,
			&d.ExpiresAt, &d.LastSeenAt, &d.CreatedAt, &d.RevokedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) RevokeTrustedDevice(ctx context.Context, id, userID uuid.UUID) error {
	tag, err := s.DB.Exec(ctx,
		`UPDATE user_trusted_devices SET revoked_at = now() WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTrustedDeviceNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tenant-level enforcement toggle.
// ---------------------------------------------------------------------------

func (s *Store) TenantRequires2FA(ctx context.Context, tenantID uuid.UUID) (bool, error) {
	var b bool
	err := s.DB.QueryRow(ctx, `SELECT require_2fa FROM tenants WHERE id = $1`, tenantID).Scan(&b)
	return b, err
}

// ---------------------------------------------------------------------------
// Errors + token helpers.
// ---------------------------------------------------------------------------

var (
	ErrInvalidRecoveryCode   = errors.New("invalid recovery code")
	ErrInvalidTrustToken     = errors.New("invalid trust token")
	ErrTrustedDeviceNotFound = errors.New("trusted device not found")
)

const trustTokenPrefixLen = 8

func generateTrustToken() (string, string) {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	h := hex.EncodeToString(b)
	return "sip_trust_" + h, h[:trustTokenPrefixLen]
}

func extractTrustPrefix(plaintext string) (string, bool) {
	const want = "sip_trust_"
	if !strings.HasPrefix(plaintext, want) {
		return "", false
	}
	rest := plaintext[len(want):]
	if len(rest) < trustTokenPrefixLen {
		return "", false
	}
	return rest[:trustTokenPrefixLen], true
}

// newRecoveryCode emits a human-friendly code: 10 hex chars in two groups,
// shown as "abcd-1234". Matches what most users expect from Google/GitHub.
func newRecoveryCode() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	h := hex.EncodeToString(b)
	return h[:4] + "-" + h[4:]
}
