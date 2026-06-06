package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Membership is a user's relationship to a single tenant.
type Membership struct {
	UserID    uuid.UUID `json:"user_id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	Role      string    `json:"role"` // user | tenant_admin
	CreatedAt time.Time `json:"created_at"`

	// Joined fields for portal UX:
	TenantName string `json:"tenant_name,omitempty"`
	TenantSlug string `json:"tenant_slug,omitempty"`
}

func (s *Store) ListMembershipsForUser(ctx context.Context, userID uuid.UUID) ([]Membership, error) {
	const q = `
		SELECT m.user_id, m.tenant_id, m.role, m.created_at,
		       t.name, t.slug
		  FROM user_tenant_memberships m
		  JOIN tenants t ON t.id = m.tenant_id
		 WHERE m.user_id = $1 AND t.status = 'active'
		 ORDER BY t.name`
	rows, err := s.DB.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Membership
	for rows.Next() {
		var m Membership
		if err := rows.Scan(&m.UserID, &m.TenantID, &m.Role, &m.CreatedAt, &m.TenantName, &m.TenantSlug); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// TenantUser is a lightweight member-of-tenant row for pickers (e.g. assigning
// an extension owner).
type TenantUser struct {
	ID          uuid.UUID
	Email       string
	DisplayName string
	Role        string
}

// ListUsersForTenant returns the active users who are members of a tenant,
// ordered by display name (falling back to email).
func (s *Store) ListUsersForTenant(ctx context.Context, tenantID uuid.UUID) ([]TenantUser, error) {
	const q = `
		SELECT u.id, u.email::text, COALESCE(u.display_name,''), m.role
		  FROM user_tenant_memberships m
		  JOIN users u ON u.id = m.user_id
		 WHERE m.tenant_id = $1 AND u.status = 'active'
		 ORDER BY COALESCE(NULLIF(u.display_name,''), u.email::text)`
	rows, err := s.DB.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TenantUser
	for rows.Next() {
		var u TenantUser
		if err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// GetMembership returns the (user, tenant) membership row if present.
// Used by the tenant-switcher to confirm a user belongs to the target tenant.
func (s *Store) GetMembership(ctx context.Context, userID, tenantID uuid.UUID) (*Membership, error) {
	const q = `
		SELECT m.user_id, m.tenant_id, m.role, m.created_at, t.name, t.slug
		  FROM user_tenant_memberships m
		  JOIN tenants t ON t.id = m.tenant_id
		 WHERE m.user_id = $1 AND m.tenant_id = $2`
	var m Membership
	err := s.DB.QueryRow(ctx, q, userID, tenantID).Scan(
		&m.UserID, &m.TenantID, &m.Role, &m.CreatedAt, &m.TenantName, &m.TenantSlug,
	)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// AddMembership grants a user a role in a tenant. Idempotent on (user, tenant).
func (s *Store) AddMembership(ctx context.Context, userID, tenantID uuid.UUID, role string) error {
	if role == "" {
		role = "user"
	}
	_, err := s.DB.Exec(ctx, `
		INSERT INTO user_tenant_memberships (user_id, tenant_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, tenant_id) DO UPDATE SET role = EXCLUDED.role`,
		userID, tenantID, role)
	return err
}
