package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type Tenant struct {
	ID           uuid.UUID  `json:"id"`
	Slug         string     `json:"slug"`
	Name         string     `json:"name"`
	Status       string     `json:"status"`
	Plan         string     `json:"plan"`
	BillingEmail string     `json:"billing_email,omitempty"`
	BillingPhone string     `json:"billing_phone,omitempty"`
	AlertEmail   string     `json:"alert_email,omitempty"`
	DailyDigest  bool       `json:"daily_digest"`
	TrialEndsAt  *time.Time `json:"trial_ends_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type SIPDomain struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	Domain    string    `json:"domain"`
	IsPrimary bool      `json:"is_primary"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) CreateTenant(ctx context.Context, slug, name string) (*Tenant, error) {
	const q = `
		INSERT INTO tenants (slug, name)
		VALUES ($1, $2)
		RETURNING id, slug, name, status, plan,
		          COALESCE(billing_email::text,''), COALESCE(billing_phone,''),
		          trial_ends_at, created_at, updated_at`
	var t Tenant
	err := s.DB.QueryRow(ctx, q, slug, name).Scan(
		&t.ID, &t.Slug, &t.Name, &t.Status, &t.Plan,
		&t.BillingEmail, &t.BillingPhone, &t.TrialEndsAt,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetTenantBySIPDomain resolves a tenant from one of its SIP domains —
// used by the dialplan handler to pick the right outbound trunk for the
// calling extension's tenant (Phase 5.1).
func (s *Store) GetTenantBySIPDomain(ctx context.Context, sipDomain string) (*Tenant, error) {
	const q = `
		SELECT t.id, t.slug, t.name, t.status, t.plan,
		       COALESCE(t.billing_email::text,''), COALESCE(t.billing_phone,''),
		       t.trial_ends_at, t.created_at, t.updated_at
		  FROM sip_domains d
		  JOIN tenants t ON t.id = d.tenant_id
		 WHERE d.domain = $1
		 LIMIT 1`
	var t Tenant
	err := s.DB.QueryRow(ctx, q, sipDomain).Scan(
		&t.ID, &t.Slug, &t.Name, &t.Status, &t.Plan,
		&t.BillingEmail, &t.BillingPhone, &t.TrialEndsAt,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetTenantBySlug is the lookup used by the per-tenant SSO entry URL.
func (s *Store) GetTenantBySlug(ctx context.Context, slug string) (*Tenant, error) {
	const q = `SELECT id, slug, name, status, plan,
	                  COALESCE(billing_email::text,''), COALESCE(billing_phone,''),
	                  trial_ends_at, created_at, updated_at
	             FROM tenants WHERE slug = $1`
	var t Tenant
	err := s.DB.QueryRow(ctx, q, slug).Scan(
		&t.ID, &t.Slug, &t.Name, &t.Status, &t.Plan,
		&t.BillingEmail, &t.BillingPhone, &t.TrialEndsAt,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// UpdateTenantAlertEmail sets (or clears, with "") the per-tenant alert
// recipient override.
func (s *Store) UpdateTenantAlertEmail(ctx context.Context, id uuid.UUID, email string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE tenants SET alert_email = NULLIF($2,''), updated_at = now() WHERE id = $1`,
		id, email)
	return err
}

// UpdateTenantDigest toggles the per-tenant daily call-summary digest.
func (s *Store) UpdateTenantDigest(ctx context.Context, id uuid.UUID, enabled bool) error {
	_, err := s.DB.Exec(ctx, `UPDATE tenants SET daily_digest = $2, updated_at = now() WHERE id = $1`, id, enabled)
	return err
}

// DigestTenant is a tenant due for a daily digest, with its alert override.
type DigestTenant struct {
	ID         uuid.UUID
	Name       string
	AlertEmail string
}

// ListDigestTenantsDue returns digest-enabled tenants not yet sent today.
func (s *Store) ListDigestTenantsDue(ctx context.Context, today time.Time) ([]DigestTenant, error) {
	const q = `
		SELECT id, name, COALESCE(alert_email,'')
		  FROM tenants
		 WHERE daily_digest = true AND status = 'active'
		   AND (last_digest_on IS NULL OR last_digest_on < $1::date)`
	rows, err := s.DB.Query(ctx, q, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DigestTenant
	for rows.Next() {
		var d DigestTenant
		if err := rows.Scan(&d.ID, &d.Name, &d.AlertEmail); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// MarkDigestSent records that a tenant's digest was sent for the given day.
func (s *Store) MarkDigestSent(ctx context.Context, id uuid.UUID, day time.Time) error {
	_, err := s.DB.Exec(ctx, `UPDATE tenants SET last_digest_on = $2::date WHERE id = $1`, id, day)
	return err
}

func (s *Store) GetTenant(ctx context.Context, id uuid.UUID) (*Tenant, error) {
	const q = `SELECT id, slug, name, status, plan,
	                  COALESCE(billing_email::text,''), COALESCE(billing_phone,''),
	                  COALESCE(alert_email,''), daily_digest,
	                  trial_ends_at, created_at, updated_at
	             FROM tenants WHERE id = $1`
	var t Tenant
	err := s.DB.QueryRow(ctx, q, id).Scan(
		&t.ID, &t.Slug, &t.Name, &t.Status, &t.Plan,
		&t.BillingEmail, &t.BillingPhone, &t.AlertEmail, &t.DailyDigest, &t.TrialEndsAt,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) ListTenants(ctx context.Context) ([]Tenant, error) {
	const q = `SELECT id, slug, name, status, plan,
	                  COALESCE(billing_email::text,''), COALESCE(billing_phone,''),
	                  trial_ends_at, created_at, updated_at
	            FROM tenants ORDER BY created_at DESC`
	rows, err := s.DB.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Slug, &t.Name, &t.Status, &t.Plan,
			&t.BillingEmail, &t.BillingPhone, &t.TrialEndsAt,
			&t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SignupInput is the enterprise self-serve signup payload.
type SignupInput struct {
	CompanyName  string
	Slug         string // auto-derived from company if empty
	Plan         string // default "trial"
	BillingEmail string
	BillingPhone string

	AdminEmail       string
	AdminPassword    string
	AdminDisplayName string
}

// CreateTenantWithAdmin runs the whole signup in one transaction:
//  1. Insert tenant (with billing + plan).
//  2. Insert user (no users.tenant_id — multi-tenant memberships only).
//  3. Insert membership(user, tenant, tenant_admin).
//
// Returns the tenant and user. Used by /v1/signup.
func (s *Store) CreateTenantWithAdmin(ctx context.Context, in SignupInput) (*Tenant, *User, error) {
	if in.CompanyName == "" || in.AdminEmail == "" || in.AdminPassword == "" {
		return nil, nil, fmt.Errorf("company_name, admin_email, admin_password required")
	}
	if in.Plan == "" {
		in.Plan = "trial"
	}
	if in.Slug == "" {
		in.Slug = autoSlug(in.CompanyName)
	}
	if in.AdminDisplayName == "" {
		in.AdminDisplayName = in.AdminEmail
	}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const tQ = `
		INSERT INTO tenants (slug, name, plan, billing_email, billing_phone, trial_ends_at)
		VALUES ($1, $2, $3, NULLIF($4,'')::citext, NULLIF($5,''),
		        CASE WHEN $3 = 'trial' THEN now() + INTERVAL '14 days' ELSE NULL END)
		RETURNING id, slug, name, status, plan,
		          COALESCE(billing_email::text,''), COALESCE(billing_phone,''),
		          trial_ends_at, created_at, updated_at`
	var t Tenant
	if err := tx.QueryRow(ctx, tQ,
		in.Slug, in.CompanyName, in.Plan, in.BillingEmail, in.BillingPhone,
	).Scan(
		&t.ID, &t.Slug, &t.Name, &t.Status, &t.Plan,
		&t.BillingEmail, &t.BillingPhone, &t.TrialEndsAt,
		&t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return nil, nil, fmt.Errorf("create tenant: %w", err)
	}

	hash, err := bcryptHash(in.AdminPassword)
	if err != nil {
		return nil, nil, err
	}
	const uQ = `
		INSERT INTO users (tenant_id, email, display_name, password_hash, role)
		VALUES (NULL, $1, $2, $3, 'user')
		RETURNING id, tenant_id, email::text, display_name, role, status, created_at, updated_at`
	var u User
	if err := tx.QueryRow(ctx, uQ, in.AdminEmail, in.AdminDisplayName, hash).Scan(
		&u.ID, &u.TenantID, &u.Email, &u.DisplayName, &u.Role,
		&u.Status, &u.CreatedAt, &u.UpdatedAt,
	); err != nil {
		return nil, nil, fmt.Errorf("create user: %w", err)
	}

	const mQ = `INSERT INTO user_tenant_memberships (user_id, tenant_id, role)
	            VALUES ($1, $2, 'tenant_admin')`
	if _, err := tx.Exec(ctx, mQ, u.ID, t.ID); err != nil {
		return nil, nil, fmt.Errorf("create membership: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	return &t, &u, nil
}

// autoSlug derives a URL-safe slug from a company name. Lowercase, dashes
// for spaces, strips non-alphanumeric. Falls back to a random suffix if
// empty after stripping.
func autoSlug(name string) string {
	var b strings.Builder
	prevDash := true
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		s = "tenant"
	}
	return s
}

func bcryptHash(plain string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(h), err
}

func (s *Store) CreateSIPDomain(ctx context.Context, tenantID uuid.UUID, domain string, primary bool) (*SIPDomain, error) {
	const q = `
		INSERT INTO sip_domains (tenant_id, domain, is_primary)
		VALUES ($1, $2, $3)
		RETURNING id, tenant_id, domain, is_primary, created_at`
	var d SIPDomain
	err := s.DB.QueryRow(ctx, q, tenantID, domain, primary).Scan(
		&d.ID, &d.TenantID, &d.Domain, &d.IsPrimary, &d.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) PrimaryDomainForTenant(ctx context.Context, tenantID uuid.UUID) (*SIPDomain, error) {
	const q = `SELECT id, tenant_id, domain, is_primary, created_at
	            FROM sip_domains WHERE tenant_id = $1 AND is_primary = true LIMIT 1`
	var d SIPDomain
	err := s.DB.QueryRow(ctx, q, tenantID).Scan(
		&d.ID, &d.TenantID, &d.Domain, &d.IsPrimary, &d.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// GetTenantMoH returns a tenant's custom Music-on-Hold URI (” = use default).
func (s *Store) GetTenantMoH(ctx context.Context, tenantID uuid.UUID) (string, error) {
	var moh string
	err := s.DB.QueryRow(ctx,
		`SELECT COALESCE(moh_url,'') FROM tenants WHERE id = $1`, tenantID).Scan(&moh)
	return moh, err
}

// SetTenantMoH updates a tenant's Music-on-Hold URI (” clears it).
func (s *Store) SetTenantMoH(ctx context.Context, tenantID uuid.UUID, moh string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE tenants SET moh_url = NULLIF($2,''), updated_at = now() WHERE id = $1`,
		tenantID, moh)
	return err
}

// TenantMoHByDomain resolves a SIP domain to the owning tenant's MoH URI.
// Best-effort for the dialplan: returns ” (the platform default) on any miss.
func (s *Store) TenantMoHByDomain(ctx context.Context, sipDomain string) string {
	var moh string
	err := s.DB.QueryRow(ctx, `
		SELECT COALESCE(t.moh_url,'')
		  FROM tenants t JOIN sip_domains sd ON sd.tenant_id = t.id
		 WHERE sd.domain = $1 LIMIT 1`, sipDomain).Scan(&moh)
	if err != nil {
		return ""
	}
	return moh
}
