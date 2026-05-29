package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type APIToken struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    *uuid.UUID `json:"tenant_id,omitempty"` // nil = super-admin
	Name        string     `json:"name"`
	TokenPrefix string     `json:"token_prefix"` // for display
	Scope       string     `json:"scope"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// IssuedAPIToken is what CreateAPIToken returns — includes the plaintext
// token (shown to the admin exactly once on create).
type IssuedAPIToken struct {
	APIToken
	Plaintext string `json:"token"` // sip_<48hex>
}

type CreateAPITokenInput struct {
	TenantID  *uuid.UUID
	Name      string
	Scope     string // read|write|admin (default write)
	ExpiresAt *time.Time
}

// CreateAPIToken generates a new token (sip_<48hex>), bcrypts the hash,
// inserts the row, and returns the plaintext exactly once.
//
// If a row with the same name+tenant already exists, returns an error —
// caller should pick a fresh name (we deliberately don't auto-rotate).
func (s *Store) CreateAPIToken(ctx context.Context, in CreateAPITokenInput) (*IssuedAPIToken, error) {
	if in.Scope == "" {
		in.Scope = "write"
	}
	plaintext, prefix := generateTokenString()
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	const q = `
		INSERT INTO api_tokens (tenant_id, name, token_prefix, token_hash, scope, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, tenant_id, name, token_prefix, scope, expires_at, last_used_at, created_at`
	var t APIToken
	err = s.DB.QueryRow(ctx, q,
		in.TenantID, in.Name, prefix, string(hash), in.Scope, in.ExpiresAt,
	).Scan(
		&t.ID, &t.TenantID, &t.Name, &t.TokenPrefix, &t.Scope,
		&t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &IssuedAPIToken{APIToken: t, Plaintext: plaintext}, nil
}

// VerifyAPIToken extracts the prefix from a plaintext token, narrows to a
// small candidate set in Postgres, and bcrypt-compares. Returns the matching
// row on success, ErrInvalidToken on no match, ErrExpiredToken if matched
// but past its expires_at.
//
// Caller should fire UpdateLastUsedAt asynchronously after a successful verify.
func (s *Store) VerifyAPIToken(ctx context.Context, plaintext string) (*APIToken, error) {
	prefix, ok := extractTokenPrefix(plaintext)
	if !ok {
		return nil, ErrInvalidToken
	}

	const q = `
		SELECT id, tenant_id, name, token_prefix, token_hash, scope,
		       expires_at, last_used_at, created_at
		  FROM api_tokens WHERE token_prefix = $1`
	rows, err := s.DB.Query(ctx, q, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			t    APIToken
			hash string
		)
		if err := rows.Scan(
			&t.ID, &t.TenantID, &t.Name, &t.TokenPrefix, &hash, &t.Scope,
			&t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt,
		); err != nil {
			return nil, err
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) != nil {
			continue
		}
		if t.ExpiresAt != nil && t.ExpiresAt.Before(time.Now()) {
			return nil, ErrExpiredToken
		}
		return &t, nil
	}
	return nil, ErrInvalidToken
}

func (s *Store) ListAPITokens(ctx context.Context) ([]APIToken, error) {
	const q = `
		SELECT id, tenant_id, name, token_prefix, scope, expires_at, last_used_at, created_at
		  FROM api_tokens ORDER BY created_at DESC`
	rows, err := s.DB.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(
			&t.ID, &t.TenantID, &t.Name, &t.TokenPrefix, &t.Scope,
			&t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) RevokeAPIToken(ctx context.Context, id uuid.UUID) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM api_tokens WHERE id = $1`, id)
	return err
}

func (s *Store) UpdateAPITokenLastUsed(ctx context.Context, id uuid.UUID) error {
	_, err := s.DB.Exec(ctx, `UPDATE api_tokens SET last_used_at = now() WHERE id = $1`, id)
	return err
}

// CountAPITokens returns the total number of tokens — used by the bootstrap
// flow to seed a token only when none exist.
func (s *Store) CountAPITokens(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRow(ctx, `SELECT COUNT(*) FROM api_tokens`).Scan(&n)
	return n, err
}

// BootstrapAPIToken inserts a token from a known plaintext (typed by the
// operator via BOOTSTRAP_API_TOKEN env). Used once at startup when the
// api_tokens table is empty, so admins can begin issuing scoped tokens
// without poking the DB.
func (s *Store) BootstrapAPIToken(ctx context.Context, plaintext, name string) (*APIToken, error) {
	prefix, ok := extractTokenPrefix(plaintext)
	if !ok {
		return nil, ErrInvalidToken
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	const q = `
		INSERT INTO api_tokens (tenant_id, name, token_prefix, token_hash, scope)
		VALUES (NULL, $1, $2, $3, 'admin')
		RETURNING id, tenant_id, name, token_prefix, scope, expires_at, last_used_at, created_at`
	var t APIToken
	err = s.DB.QueryRow(ctx, q, name, prefix, string(hash)).Scan(
		&t.ID, &t.TenantID, &t.Name, &t.TokenPrefix, &t.Scope,
		&t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Errors --------------------------------------------------------------------

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token expired")
)

// Token format helpers -----------------------------------------------------

const tokenPrefixLen = 8 // chars after "sip_"

// generateTokenString returns ("sip_<48hex>", "<first 8 hex>")
func generateTokenString() (string, string) {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	hex := hex.EncodeToString(b)
	return "sip_" + hex, hex[:tokenPrefixLen]
}

// extractTokenPrefix returns the prefix used for narrowing the bcrypt
// candidate set. Returns false if the input doesn't look like one of our
// tokens at all.
func extractTokenPrefix(plaintext string) (string, bool) {
	const want = "sip_"
	if len(plaintext) < len(want)+tokenPrefixLen {
		return "", false
	}
	if plaintext[:len(want)] != want {
		return "", false
	}
	return plaintext[len(want) : len(want)+tokenPrefixLen], true
}
