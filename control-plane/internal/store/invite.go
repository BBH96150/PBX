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

// Invite is a pending invitation for `email` to join `tenant_id` with `role`.
// Tokens are stored as bcrypt hashes; the plaintext is sent to the invitee
// in the email and never persisted.
type Invite struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	Email       string     `json:"email"`
	Role        string     `json:"role"`
	TokenPrefix string     `json:"token_prefix"`
	InvitedBy   *uuid.UUID `json:"invited_by,omitempty"`
	ExpiresAt   time.Time  `json:"expires_at"`
	AcceptedAt  *time.Time `json:"accepted_at,omitempty"`
	AcceptedBy  *uuid.UUID `json:"accepted_by,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// IssuedInvite is the create result — plaintext token included exactly once
// so the API can email it.
type IssuedInvite struct {
	Invite
	Plaintext string `json:"token"`
}

type CreateInviteInput struct {
	TenantID    uuid.UUID
	Email       string
	Role        string // "user" or "tenant_admin"
	InvitedBy   *uuid.UUID
	ExpiresIn   time.Duration // default 7 days
}

func (s *Store) CreateInvite(ctx context.Context, in CreateInviteInput) (*IssuedInvite, error) {
	if in.Email == "" {
		return nil, errors.New("email required")
	}
	if in.Role == "" {
		in.Role = "user"
	}
	plaintext, prefix := generateInviteToken()
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	expires := time.Now().Add(7 * 24 * time.Hour)
	if in.ExpiresIn > 0 {
		expires = time.Now().Add(in.ExpiresIn)
	}
	const q = `
		INSERT INTO user_invites
		    (tenant_id, email, role, token_prefix, token_hash, invited_by, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, tenant_id, email::text, role, token_prefix,
		          invited_by, expires_at, accepted_at, accepted_by, revoked_at, created_at`
	var inv Invite
	err = s.DB.QueryRow(ctx, q,
		in.TenantID, in.Email, in.Role, prefix, string(hash), in.InvitedBy, expires,
	).Scan(
		&inv.ID, &inv.TenantID, &inv.Email, &inv.Role, &inv.TokenPrefix,
		&inv.InvitedBy, &inv.ExpiresAt, &inv.AcceptedAt, &inv.AcceptedBy,
		&inv.RevokedAt, &inv.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &IssuedInvite{Invite: inv, Plaintext: plaintext}, nil
}

// VerifyInvite extracts the prefix, narrows the candidate set, bcrypt-checks,
// and returns the invite row. Returns ErrInvalidInviteToken on any failure;
// ErrInviteExpired if it matched but is past expiry; ErrInviteUsed if it
// was already accepted; ErrInviteRevoked if revoked.
func (s *Store) VerifyInvite(ctx context.Context, plaintext string) (*Invite, error) {
	prefix, ok := extractInvitePrefix(plaintext)
	if !ok {
		return nil, ErrInvalidInviteToken
	}
	const q = `
		SELECT id, tenant_id, email::text, role, token_prefix, token_hash,
		       invited_by, expires_at, accepted_at, accepted_by, revoked_at, created_at
		  FROM user_invites WHERE token_prefix = $1`
	rows, err := s.DB.Query(ctx, q, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var inv Invite
		var hash string
		if err := rows.Scan(
			&inv.ID, &inv.TenantID, &inv.Email, &inv.Role, &inv.TokenPrefix, &hash,
			&inv.InvitedBy, &inv.ExpiresAt, &inv.AcceptedAt, &inv.AcceptedBy,
			&inv.RevokedAt, &inv.CreatedAt,
		); err != nil {
			return nil, err
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) != nil {
			continue
		}
		if inv.RevokedAt != nil {
			return nil, ErrInviteRevoked
		}
		if inv.AcceptedAt != nil {
			return nil, ErrInviteUsed
		}
		if inv.ExpiresAt.Before(time.Now()) {
			return nil, ErrInviteExpired
		}
		return &inv, nil
	}
	return nil, ErrInvalidInviteToken
}

// AcceptInvite runs the accept transaction:
//   1. Re-verify invite.
//   2. If a user with this email exists, add a membership to the inviting tenant.
//      Otherwise create the user with the supplied password + add the membership.
//   3. Mark the invite accepted.
//
// Returns the user that the inviter can now session-as.
func (s *Store) AcceptInvite(ctx context.Context, plaintext, displayName, password string) (*User, error) {
	inv, err := s.VerifyInvite(ctx, plaintext)
	if err != nil {
		return nil, err
	}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1. Existing user with this email?
	var (
		userID uuid.UUID
		exists bool
	)
	err = tx.QueryRow(ctx, `SELECT id FROM users WHERE email = $1::citext LIMIT 1`, inv.Email).Scan(&userID)
	if err == nil {
		exists = true
	}

	if !exists {
		if password == "" {
			return nil, errors.New("password required for new user")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		if displayName == "" {
			displayName = inv.Email
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO users (tenant_id, email, display_name, password_hash, role, email_verified_at)
			VALUES (NULL, $1, $2, $3, 'user', now())
			RETURNING id`,
			inv.Email, displayName, string(hash),
		).Scan(&userID); err != nil {
			return nil, err
		}
	}

	// 2. Membership.
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_tenant_memberships (user_id, tenant_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, tenant_id) DO UPDATE SET role = EXCLUDED.role`,
		userID, inv.TenantID, inv.Role,
	); err != nil {
		return nil, err
	}

	// 3. Mark accepted.
	if _, err := tx.Exec(ctx, `
		UPDATE user_invites SET accepted_at = now(), accepted_by = $2
		 WHERE id = $1`, inv.ID, userID); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	// Return the now-current user.
	return s.GetUserByID(ctx, userID)
}

// GetUserByID — small helper used by invite + reset flows.
func (s *Store) GetUserByID(ctx context.Context, id uuid.UUID) (*User, error) {
	const q = `
		SELECT id, tenant_id, email::text, display_name, role, status,
		       email_verified_at, created_at, updated_at
		  FROM users WHERE id = $1`
	var u User
	err := s.DB.QueryRow(ctx, q, id).Scan(
		&u.ID, &u.TenantID, &u.Email, &u.DisplayName, &u.Role,
		&u.Status, &u.EmailVerifiedAt, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) ListInvitesForTenant(ctx context.Context, tenantID uuid.UUID) ([]Invite, error) {
	const q = `
		SELECT id, tenant_id, email::text, role, token_prefix,
		       invited_by, expires_at, accepted_at, accepted_by, revoked_at, created_at
		  FROM user_invites WHERE tenant_id = $1
		 ORDER BY created_at DESC`
	rows, err := s.DB.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invite
	for rows.Next() {
		var i Invite
		if err := rows.Scan(
			&i.ID, &i.TenantID, &i.Email, &i.Role, &i.TokenPrefix,
			&i.InvitedBy, &i.ExpiresAt, &i.AcceptedAt, &i.AcceptedBy,
			&i.RevokedAt, &i.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) RevokeInvite(ctx context.Context, id uuid.UUID) error {
	_, err := s.DB.Exec(ctx, `UPDATE user_invites SET revoked_at = now() WHERE id = $1 AND accepted_at IS NULL`, id)
	return err
}

// Errors -------------------------------------------------------------------

var (
	ErrInvalidInviteToken = errors.New("invalid invite token")
	ErrInviteExpired      = errors.New("invite expired")
	ErrInviteUsed         = errors.New("invite already used")
	ErrInviteRevoked      = errors.New("invite revoked")
)

// Token helpers ------------------------------------------------------------

const inviteTokenPrefixLen = 8

func generateInviteToken() (string, string) {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	h := hex.EncodeToString(b)
	return "sip_invite_" + h, h[:inviteTokenPrefixLen]
}

func extractInvitePrefix(plaintext string) (string, bool) {
	const want = "sip_invite_"
	if !strings.HasPrefix(plaintext, want) {
		return "", false
	}
	rest := plaintext[len(want):]
	if len(rest) < inviteTokenPrefixLen {
		return "", false
	}
	return rest[:inviteTokenPrefixLen], true
}
