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

// PasswordResetToken — one-use, 2-hour-lived. Plaintext shape:
//   sip_reset_<48hex>
type PasswordResetToken struct {
	ID          uuid.UUID  `json:"id"`
	UserID      uuid.UUID  `json:"user_id"`
	TokenPrefix string     `json:"token_prefix"`
	ExpiresAt   time.Time  `json:"expires_at"`
	UsedAt      *time.Time `json:"used_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type IssuedResetToken struct {
	PasswordResetToken
	Plaintext string `json:"token"`
}

// CreatePasswordResetToken generates a token for an email. To avoid leaking
// account existence, callers should call this regardless of whether the
// email matches a user — but here we surface the lookup failure since the
// portal handler will swallow it.
func (s *Store) CreatePasswordResetToken(ctx context.Context, email string) (*IssuedResetToken, error) {
	user, err := s.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}

	plaintext, prefix := generateResetToken()
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	expires := time.Now().Add(2 * time.Hour)

	const q = `
		INSERT INTO password_reset_tokens (user_id, token_prefix, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id, user_id, token_prefix, expires_at, used_at, created_at`
	var t PasswordResetToken
	err = s.DB.QueryRow(ctx, q, user.ID, prefix, string(hash), expires).Scan(
		&t.ID, &t.UserID, &t.TokenPrefix, &t.ExpiresAt, &t.UsedAt, &t.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &IssuedResetToken{PasswordResetToken: t, Plaintext: plaintext}, nil
}

// ConsumeResetToken validates a plaintext token, sets the user's password
// to `newPassword`, marks the token used, and returns the user. Single-use.
func (s *Store) ConsumeResetToken(ctx context.Context, plaintext, newPassword string) (*User, error) {
	if newPassword == "" {
		return nil, errors.New("new password required")
	}
	prefix, ok := extractResetPrefix(plaintext)
	if !ok {
		return nil, ErrInvalidResetToken
	}

	const q = `
		SELECT id, user_id, token_prefix, token_hash, expires_at, used_at
		  FROM password_reset_tokens WHERE token_prefix = $1`
	rows, err := s.DB.Query(ctx, q, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var match *PasswordResetToken
	for rows.Next() {
		var t PasswordResetToken
		var hash string
		if err := rows.Scan(&t.ID, &t.UserID, &t.TokenPrefix, &hash, &t.ExpiresAt, &t.UsedAt); err != nil {
			return nil, err
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) != nil {
			continue
		}
		if t.UsedAt != nil {
			return nil, ErrResetTokenUsed
		}
		if t.ExpiresAt.Before(time.Now()) {
			return nil, ErrResetTokenExpired
		}
		match = &t
		break
	}
	rows.Close()
	if match == nil {
		return nil, ErrInvalidResetToken
	}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, match.UserID, string(newHash)); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE password_reset_tokens SET used_at = now() WHERE id = $1`, match.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetUserByID(ctx, match.UserID)
}

var (
	ErrInvalidResetToken = errors.New("invalid reset token")
	ErrResetTokenExpired = errors.New("reset token expired")
	ErrResetTokenUsed    = errors.New("reset token already used")
)

const resetTokenPrefixLen = 8

func generateResetToken() (string, string) {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	h := hex.EncodeToString(b)
	return "sip_reset_" + h, h[:resetTokenPrefixLen]
}

func extractResetPrefix(plaintext string) (string, bool) {
	const want = "sip_reset_"
	if !strings.HasPrefix(plaintext, want) {
		return "", false
	}
	rest := plaintext[len(want):]
	if len(rest) < resetTokenPrefixLen {
		return "", false
	}
	return rest[:resetTokenPrefixLen], true
}
