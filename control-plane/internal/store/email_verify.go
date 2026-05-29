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

// EmailVerificationToken — 24h-lived, single-use. Plaintext shape:
//   sip_verify_<48hex>
type EmailVerificationToken struct {
	ID          uuid.UUID  `json:"id"`
	UserID      uuid.UUID  `json:"user_id"`
	TokenPrefix string     `json:"token_prefix"`
	ExpiresAt   time.Time  `json:"expires_at"`
	ConsumedAt  *time.Time `json:"consumed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type IssuedVerificationToken struct {
	EmailVerificationToken
	Plaintext string `json:"token"`
}

// CreateEmailVerificationToken issues a fresh token. Rate-limited: refuses
// if any token for this user was created in the last `cooldown`. Returns
// ErrVerificationCooldown so callers can render a friendly message.
//
// Pass cooldown = 0 to skip the check (signup path).
func (s *Store) CreateEmailVerificationToken(ctx context.Context, userID uuid.UUID, cooldown time.Duration) (*IssuedVerificationToken, error) {
	if cooldown > 0 {
		var newest *time.Time
		if err := s.DB.QueryRow(ctx,
			`SELECT MAX(created_at) FROM email_verification_tokens
			  WHERE user_id = $1 AND consumed_at IS NULL`,
			userID,
		).Scan(&newest); err != nil {
			return nil, err
		}
		if newest != nil && time.Since(*newest) < cooldown {
			return nil, ErrVerificationCooldown
		}
	}

	plaintext, prefix := generateVerificationToken()
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	const q = `
		INSERT INTO email_verification_tokens (user_id, token_prefix, token_hash)
		VALUES ($1, $2, $3)
		RETURNING id, user_id, token_prefix, expires_at, consumed_at, created_at`
	var t EmailVerificationToken
	err = s.DB.QueryRow(ctx, q, userID, prefix, string(hash)).Scan(
		&t.ID, &t.UserID, &t.TokenPrefix, &t.ExpiresAt, &t.ConsumedAt, &t.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &IssuedVerificationToken{EmailVerificationToken: t, Plaintext: plaintext}, nil
}

// ConsumeEmailVerificationToken validates the plaintext, marks the token
// consumed, and flips users.email_verified_at to now() if still NULL.
// Returns the user that was verified (callers may auto-mount a session).
func (s *Store) ConsumeEmailVerificationToken(ctx context.Context, plaintext string) (*User, error) {
	prefix, ok := extractVerificationPrefix(plaintext)
	if !ok {
		return nil, ErrInvalidVerificationToken
	}

	const q = `
		SELECT id, user_id, token_prefix, token_hash, expires_at, consumed_at
		  FROM email_verification_tokens WHERE token_prefix = $1`
	rows, err := s.DB.Query(ctx, q, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var match *EmailVerificationToken
	for rows.Next() {
		var t EmailVerificationToken
		var hash string
		if err := rows.Scan(&t.ID, &t.UserID, &t.TokenPrefix, &hash, &t.ExpiresAt, &t.ConsumedAt); err != nil {
			return nil, err
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) != nil {
			continue
		}
		if t.ConsumedAt != nil {
			return nil, ErrVerificationTokenUsed
		}
		if t.ExpiresAt.Before(time.Now()) {
			return nil, ErrVerificationTokenExpired
		}
		match = &t
		break
	}
	rows.Close()
	if match == nil {
		return nil, ErrInvalidVerificationToken
	}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`UPDATE email_verification_tokens SET consumed_at = now() WHERE id = $1`,
		match.ID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE users SET email_verified_at = now() WHERE id = $1 AND email_verified_at IS NULL`,
		match.UserID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetUserByID(ctx, match.UserID)
}

var (
	ErrInvalidVerificationToken = errors.New("invalid verification token")
	ErrVerificationTokenExpired = errors.New("verification token expired")
	ErrVerificationTokenUsed    = errors.New("verification token already used")
	ErrVerificationCooldown     = errors.New("verification email was sent recently; try again in a minute")
)

const verificationTokenPrefixLen = 8

func generateVerificationToken() (string, string) {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	h := hex.EncodeToString(b)
	return "sip_verify_" + h, h[:verificationTokenPrefixLen]
}

func extractVerificationPrefix(plaintext string) (string, bool) {
	const want = "sip_verify_"
	if !strings.HasPrefix(plaintext, want) {
		return "", false
	}
	rest := plaintext[len(want):]
	if len(rest) < verificationTokenPrefixLen {
		return "", false
	}
	return rest[:verificationTokenPrefixLen], true
}
