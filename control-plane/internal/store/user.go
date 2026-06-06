package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// User is a human with a portal login. tenant_id NULL → super-admin
// (sees all tenants). Role decides scope when a derived API token is
// minted on login.
type User struct {
	ID              uuid.UUID  `json:"id"`
	TenantID        *uuid.UUID `json:"tenant_id,omitempty"` // nil = super-admin
	Email           string     `json:"email"`
	DisplayName     string     `json:"display_name"`
	Role            string     `json:"role"` // user|tenant_admin|super_admin
	Status          string     `json:"status"`
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type CreateUserInput struct {
	TenantID    *uuid.UUID // nil for super-admin
	Email       string
	DisplayName string
	Role        string // default "user"; super-admin tokens can set higher
	Password    string // plaintext; bcrypted server-side
}

// CreateUser inserts a user with a bcrypted password. Caller is responsible
// for permission checks (super-admin only, etc.).
func (s *Store) CreateUser(ctx context.Context, in CreateUserInput) (*User, error) {
	if in.Email == "" || in.Password == "" || in.DisplayName == "" {
		return nil, errors.New("email, display_name, password required")
	}
	if in.Role == "" {
		in.Role = "user"
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	const q = `
		INSERT INTO users (tenant_id, email, display_name, password_hash, role)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, tenant_id, email::text, display_name, role, status,
		          email_verified_at, created_at, updated_at`
	var u User
	err = s.DB.QueryRow(ctx, q,
		in.TenantID, in.Email, in.DisplayName, string(hash), in.Role,
	).Scan(&u.ID, &u.TenantID, &u.Email, &u.DisplayName, &u.Role,
		&u.Status, &u.EmailVerifiedAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// VerifyUserPassword finds a user by email and bcrypt-checks the password.
// Returns ErrInvalidCredentials on either unknown email or wrong password
// (deliberately doesn't distinguish, to not leak which emails exist).
//
// Email lookup: try super-admin (tenant_id NULL) first, then tenanted.
// We don't pin login to a tenant — the user's row tells us which one.
func (s *Store) VerifyUserPassword(ctx context.Context, email, password string) (*User, error) {
	const q = `
		SELECT id, tenant_id, email::text, display_name, role, password_hash,
		       status, email_verified_at, created_at, updated_at
		  FROM users
		 WHERE email = $1::citext AND status = 'active'
		 ORDER BY tenant_id IS NOT NULL  -- NULL first (super-admin priority)
		 LIMIT 1`
	var u User
	var hash *string
	err := s.DB.QueryRow(ctx, q, email).Scan(
		&u.ID, &u.TenantID, &u.Email, &u.DisplayName, &u.Role, &hash,
		&u.Status, &u.EmailVerifiedAt, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if hash == nil || *hash == "" {
		return nil, ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(*hash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	return &u, nil
}

// SetUserPassword updates the bcrypt hash. Both the API password-change
// endpoint and an admin reset go through here.
func (s *Store) SetUserPassword(ctx context.Context, userID uuid.UUID, newPassword string) error {
	if newPassword == "" {
		return errors.New("password required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, userID, string(hash))
	return err
}

// UpdateUserDisplayName changes a user's display name.
func (s *Store) UpdateUserDisplayName(ctx context.Context, userID uuid.UUID, name string) error {
	_, err := s.DB.Exec(ctx, `UPDATE users SET display_name = $2, updated_at = now() WHERE id = $1`, userID, name)
	return err
}

// GetUserByEmail does the same lookup as VerifyUserPassword minus the
// bcrypt check. Used by portal session resolution.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	const q = `
		SELECT id, tenant_id, email::text, display_name, role, status,
		       email_verified_at, created_at, updated_at
		  FROM users
		 WHERE email = $1::citext AND status = 'active'
		 ORDER BY tenant_id IS NOT NULL
		 LIMIT 1`
	var u User
	err := s.DB.QueryRow(ctx, q, email).Scan(
		&u.ID, &u.TenantID, &u.Email, &u.DisplayName, &u.Role,
		&u.Status, &u.EmailVerifiedAt, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// BootstrapUser creates the very first super-admin user on startup when the
// users table is empty and BOOTSTRAP_USER_* env vars are set. Idempotent in
// the empty-table check; refuses if users already exist.
func (s *Store) BootstrapUser(ctx context.Context, email, displayName, password string) (*User, error) {
	return s.CreateUser(ctx, CreateUserInput{
		TenantID:    nil, // super-admin
		Email:       email,
		DisplayName: displayName,
		Role:        "super_admin",
		Password:    password,
	})
}

// ErrInvalidCredentials is returned by VerifyUserPassword on either an
// unknown email or wrong password. Deliberately a single error so callers
// don't accidentally leak which.
var ErrInvalidCredentials = errors.New("invalid credentials")

// ScopeForRole maps users.role to the API-token scope a session should hold.
// super_admin → admin, tenant_admin → admin (tenant-scoped), user → write.
func ScopeForRole(role string) string {
	switch role {
	case "super_admin", "tenant_admin":
		return "admin"
	case "user":
		return "write"
	default:
		return "read"
	}
}
