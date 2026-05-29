package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Per-tenant SSO configuration (one OIDC IdP per tenant for now).
// ---------------------------------------------------------------------------

type TenantSSOConfig struct {
	ID                     uuid.UUID `json:"id"`
	TenantID               uuid.UUID `json:"tenant_id"`
	ProviderKind           string    `json:"provider_kind"`
	Label                  string    `json:"label"`
	IssuerURL              string    `json:"issuer_url"`
	ClientID               string    `json:"client_id"`
	ClientSecretCiphertext []byte    `json:"-"`
	ClientSecretNonce      []byte    `json:"-"`
	Scopes                 string    `json:"scopes"`
	Enabled                bool      `json:"enabled"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type SaveTenantSSOConfigInput struct {
	TenantID               uuid.UUID
	ProviderKind           string // "oidc"
	Label                  string
	IssuerURL              string
	ClientID               string
	ClientSecretCiphertext []byte
	ClientSecretNonce      []byte
	Scopes                 string
	Enabled                bool
}

// SaveTenantSSOConfig upserts on tenant_id (one config per tenant for now).
// If the secret ciphertext is empty, the existing secret is preserved — lets
// admins update non-secret fields without re-entering the client secret.
func (s *Store) SaveTenantSSOConfig(ctx context.Context, in SaveTenantSSOConfigInput) (*TenantSSOConfig, error) {
	if in.ProviderKind == "" {
		in.ProviderKind = "oidc"
	}
	if in.Scopes == "" {
		in.Scopes = "openid email profile"
	}
	const q = `
		INSERT INTO tenant_sso_configs
		    (tenant_id, provider_kind, label, issuer_url, client_id,
		     client_secret_ciphertext, client_secret_nonce, scopes, enabled)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (tenant_id) DO UPDATE SET
		    provider_kind = EXCLUDED.provider_kind,
		    label         = EXCLUDED.label,
		    issuer_url    = EXCLUDED.issuer_url,
		    client_id     = EXCLUDED.client_id,
		    client_secret_ciphertext = CASE WHEN length(EXCLUDED.client_secret_ciphertext) > 0
		                                    THEN EXCLUDED.client_secret_ciphertext
		                                    ELSE tenant_sso_configs.client_secret_ciphertext END,
		    client_secret_nonce      = CASE WHEN length(EXCLUDED.client_secret_nonce) > 0
		                                    THEN EXCLUDED.client_secret_nonce
		                                    ELSE tenant_sso_configs.client_secret_nonce END,
		    scopes        = EXCLUDED.scopes,
		    enabled       = EXCLUDED.enabled,
		    updated_at    = now()
		RETURNING id, tenant_id, provider_kind, label, issuer_url, client_id,
		          client_secret_ciphertext, client_secret_nonce, scopes, enabled,
		          created_at, updated_at`
	var c TenantSSOConfig
	err := s.DB.QueryRow(ctx, q,
		in.TenantID, in.ProviderKind, in.Label, in.IssuerURL, in.ClientID,
		in.ClientSecretCiphertext, in.ClientSecretNonce, in.Scopes, in.Enabled,
	).Scan(
		&c.ID, &c.TenantID, &c.ProviderKind, &c.Label, &c.IssuerURL, &c.ClientID,
		&c.ClientSecretCiphertext, &c.ClientSecretNonce, &c.Scopes, &c.Enabled,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// GetTenantSSOConfig returns the (single) config for a tenant, or nil if none.
func (s *Store) GetTenantSSOConfig(ctx context.Context, tenantID uuid.UUID) (*TenantSSOConfig, error) {
	const q = `
		SELECT id, tenant_id, provider_kind, label, issuer_url, client_id,
		       client_secret_ciphertext, client_secret_nonce, scopes, enabled,
		       created_at, updated_at
		  FROM tenant_sso_configs WHERE tenant_id = $1`
	var c TenantSSOConfig
	err := s.DB.QueryRow(ctx, q, tenantID).Scan(
		&c.ID, &c.TenantID, &c.ProviderKind, &c.Label, &c.IssuerURL, &c.ClientID,
		&c.ClientSecretCiphertext, &c.ClientSecretNonce, &c.Scopes, &c.Enabled,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) DisableTenantSSOConfig(ctx context.Context, tenantID uuid.UUID) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE tenant_sso_configs SET enabled = false, updated_at = now() WHERE tenant_id = $1`,
		tenantID)
	return err
}

func (s *Store) TenantRequiresSSO(ctx context.Context, tenantID uuid.UUID) (bool, error) {
	var b bool
	err := s.DB.QueryRow(ctx, `SELECT require_sso FROM tenants WHERE id = $1`, tenantID).Scan(&b)
	return b, err
}

// ---------------------------------------------------------------------------
// Domain → tenant mapping for login-page discovery.
// ---------------------------------------------------------------------------

type TenantSSODomain struct {
	ID         uuid.UUID  `json:"id"`
	TenantID   uuid.UUID  `json:"tenant_id"`
	Domain     string     `json:"domain"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (s *Store) AddTenantSSODomain(ctx context.Context, tenantID uuid.UUID, domain string) (*TenantSSODomain, error) {
	const q = `
		INSERT INTO tenant_sso_domains (tenant_id, domain)
		VALUES ($1, $2)
		RETURNING id, tenant_id, domain::text, verified_at, created_at`
	var d TenantSSODomain
	err := s.DB.QueryRow(ctx, q, tenantID, domain).Scan(
		&d.ID, &d.TenantID, &d.Domain, &d.VerifiedAt, &d.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) ListTenantSSODomains(ctx context.Context, tenantID uuid.UUID) ([]TenantSSODomain, error) {
	const q = `
		SELECT id, tenant_id, domain::text, verified_at, created_at
		  FROM tenant_sso_domains WHERE tenant_id = $1
		 ORDER BY domain`
	rows, err := s.DB.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TenantSSODomain
	for rows.Next() {
		var d TenantSSODomain
		if err := rows.Scan(&d.ID, &d.TenantID, &d.Domain, &d.VerifiedAt, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) RemoveTenantSSODomain(ctx context.Context, id, tenantID uuid.UUID) error {
	tag, err := s.DB.Exec(ctx,
		`DELETE FROM tenant_sso_domains WHERE id = $1 AND tenant_id = $2`,
		id, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("domain not found")
	}
	return nil
}

// LookupSSOByEmailDomain extracts the domain from `email` and returns the
// owning tenant + its (enabled) SSO config, or (nil, nil, nil) if no
// configured tenant claims this domain.
func (s *Store) LookupSSOByEmailDomain(ctx context.Context, email string) (*Tenant, *TenantSSOConfig, error) {
	at := -1
	for i, r := range email {
		if r == '@' {
			at = i
		}
	}
	if at < 0 || at == len(email)-1 {
		return nil, nil, nil
	}
	domain := email[at+1:]
	const q = `
		SELECT t.id, t.slug, t.name, t.status, COALESCE(t.plan,''),
		       COALESCE(t.billing_email::text,''), COALESCE(t.billing_phone,''),
		       t.trial_ends_at, t.created_at, t.updated_at,
		       c.id, c.tenant_id, c.provider_kind, c.label, c.issuer_url, c.client_id,
		       c.client_secret_ciphertext, c.client_secret_nonce, c.scopes, c.enabled,
		       c.created_at, c.updated_at
		  FROM tenant_sso_domains d
		  JOIN tenants t ON t.id = d.tenant_id
		  JOIN tenant_sso_configs c ON c.tenant_id = d.tenant_id
		 WHERE d.domain = $1::citext AND c.enabled = true
		 LIMIT 1`
	var t Tenant
	var c TenantSSOConfig
	err := s.DB.QueryRow(ctx, q, domain).Scan(
		&t.ID, &t.Slug, &t.Name, &t.Status, &t.Plan,
		&t.BillingEmail, &t.BillingPhone, &t.TrialEndsAt,
		&t.CreatedAt, &t.UpdatedAt,
		&c.ID, &c.TenantID, &c.ProviderKind, &c.Label, &c.IssuerURL, &c.ClientID,
		&c.ClientSecretCiphertext, &c.ClientSecretNonce, &c.Scopes, &c.Enabled,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, nil, nil
	}
	return &t, &c, nil
}

// ---------------------------------------------------------------------------
// User SSO identities (one user can have multiple).
// ---------------------------------------------------------------------------

type UserSSOIdentity struct {
	ID           uuid.UUID       `json:"id"`
	UserID       uuid.UUID       `json:"user_id"`
	ProviderKind string          `json:"provider_kind"`
	Issuer       string          `json:"issuer"`
	Subject      string          `json:"subject"`
	Email        string          `json:"email"`
	RawClaims    json.RawMessage `json:"raw_claims"`
	FirstSeenAt  time.Time       `json:"first_seen_at"`
	LastUsedAt   time.Time       `json:"last_used_at"`
}

type UpsertSSOIdentityInput struct {
	UserID       uuid.UUID
	ProviderKind string
	Issuer       string
	Subject      string
	Email        string
	RawClaims    map[string]any
}

// UpsertSSOIdentity matches on (provider_kind, issuer, subject). If the row
// exists, last_used_at + claims are updated. Returns the row + whether it was
// newly created (for audit differentiation).
func (s *Store) UpsertSSOIdentity(ctx context.Context, in UpsertSSOIdentityInput) (*UserSSOIdentity, bool, error) {
	if in.ProviderKind == "" {
		in.ProviderKind = "oidc"
	}
	raw, _ := json.Marshal(in.RawClaims)
	const q = `
		INSERT INTO user_sso_identities
		    (user_id, provider_kind, issuer, subject, email, raw_claims)
		VALUES ($1,$2,$3,$4,NULLIF($5,'')::citext,$6::jsonb)
		ON CONFLICT (provider_kind, issuer, subject) DO UPDATE SET
		    email        = EXCLUDED.email,
		    raw_claims   = EXCLUDED.raw_claims,
		    last_used_at = now()
		RETURNING id, user_id, provider_kind, issuer, subject,
		          COALESCE(email::text,''), raw_claims::text,
		          first_seen_at, last_used_at,
		          (xmax = 0) AS created`
	var i UserSSOIdentity
	var claims string
	var created bool
	err := s.DB.QueryRow(ctx, q,
		in.UserID, in.ProviderKind, in.Issuer, in.Subject, in.Email, string(raw),
	).Scan(
		&i.ID, &i.UserID, &i.ProviderKind, &i.Issuer, &i.Subject,
		&i.Email, &claims, &i.FirstSeenAt, &i.LastUsedAt, &created,
	)
	if err != nil {
		return nil, false, err
	}
	i.RawClaims = json.RawMessage(claims)
	return &i, created, nil
}

// FindUserBySSOIdentity is the primary login-time lookup.
func (s *Store) FindUserBySSOIdentity(ctx context.Context, providerKind, issuer, subject string) (*User, error) {
	const q = `
		SELECT u.id, u.tenant_id, u.email::text, u.display_name, u.role,
		       u.status, u.email_verified_at, u.created_at, u.updated_at
		  FROM user_sso_identities i
		  JOIN users u ON u.id = i.user_id
		 WHERE i.provider_kind = $1 AND i.issuer = $2 AND i.subject = $3
		   AND u.status = 'active'`
	var u User
	err := s.DB.QueryRow(ctx, q, providerKind, issuer, subject).Scan(
		&u.ID, &u.TenantID, &u.Email, &u.DisplayName, &u.Role,
		&u.Status, &u.EmailVerifiedAt, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) ListSSOIdentitiesForUser(ctx context.Context, userID uuid.UUID) ([]UserSSOIdentity, error) {
	const q = `
		SELECT id, user_id, provider_kind, issuer, subject,
		       COALESCE(email::text,''), raw_claims::text,
		       first_seen_at, last_used_at
		  FROM user_sso_identities WHERE user_id = $1
		 ORDER BY last_used_at DESC`
	rows, err := s.DB.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserSSOIdentity
	for rows.Next() {
		var i UserSSOIdentity
		var claims string
		if err := rows.Scan(
			&i.ID, &i.UserID, &i.ProviderKind, &i.Issuer, &i.Subject,
			&i.Email, &claims, &i.FirstSeenAt, &i.LastUsedAt,
		); err != nil {
			return nil, err
		}
		i.RawClaims = json.RawMessage(claims)
		out = append(out, i)
	}
	return out, rows.Err()
}

// UnlinkSSOIdentity removes a single identity. Refuses if it's the only
// authentication factor for the user (no password set AND no other identity).
func (s *Store) UnlinkSSOIdentity(ctx context.Context, id, userID uuid.UUID) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var hasPassword bool
	if err := tx.QueryRow(ctx,
		`SELECT password_hash IS NOT NULL AND password_hash <> '' FROM users WHERE id = $1`,
		userID).Scan(&hasPassword); err != nil {
		return err
	}
	var identityCount int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_sso_identities WHERE user_id = $1`,
		userID).Scan(&identityCount); err != nil {
		return err
	}
	if !hasPassword && identityCount <= 1 {
		return ErrLastAuthFactor
	}
	tag, err := tx.Exec(ctx,
		`DELETE FROM user_sso_identities WHERE id = $1 AND user_id = $2`,
		id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("identity not found")
	}
	return tx.Commit(ctx)
}

// ---------------------------------------------------------------------------
// JIT: create or link a user on first SSO login.
// ---------------------------------------------------------------------------

// JITProvisionInput is the bundle the SSO callback hands to the store after
// it has verified the ID token and parsed the claims.
type JITProvisionInput struct {
	TenantID     uuid.UUID
	ProviderKind string
	Issuer       string
	Subject      string
	Email        string
	DisplayName  string
	RawClaims    map[string]any
}

// JITProvisionResult: which path got taken, for audit + UX banners.
type JITProvisionResult struct {
	User       *User
	Identity   *UserSSOIdentity
	WasCreated bool // newly-created user (vs auto-linked to existing)
	WasLinked  bool // existing user, identity newly attached
}

// ProvisionFromSSO: idempotent SSO login. Path order:
//  1. Existing identity row (subject match)  → return its user
//  2. Existing user with matching email      → link identity, ensure membership
//  3. Brand-new                              → create user (verified), add membership, link identity
//
// In all three paths we ensure a membership row in the target tenant exists.
func (s *Store) ProvisionFromSSO(ctx context.Context, in JITProvisionInput) (*JITProvisionResult, error) {
	if in.ProviderKind == "" {
		in.ProviderKind = "oidc"
	}
	if in.Email == "" {
		return nil, errors.New("SSO claims must include email")
	}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	res := &JITProvisionResult{}
	rawClaims, _ := json.Marshal(in.RawClaims)

	// 1. Identity already linked?
	var userID uuid.UUID
	identityErr := tx.QueryRow(ctx, `
		SELECT user_id FROM user_sso_identities
		 WHERE provider_kind = $1 AND issuer = $2 AND subject = $3`,
		in.ProviderKind, in.Issuer, in.Subject,
	).Scan(&userID)

	if identityErr != nil {
		// 2. Existing user by email?
		emailErr := tx.QueryRow(ctx,
			`SELECT id FROM users WHERE email = $1::citext AND status = 'active' ORDER BY tenant_id IS NOT NULL LIMIT 1`,
			in.Email,
		).Scan(&userID)
		if emailErr != nil {
			// 3. Create user.
			displayName := in.DisplayName
			if displayName == "" {
				displayName = in.Email
			}
			if err := tx.QueryRow(ctx, `
				INSERT INTO users (tenant_id, email, display_name, password_hash, role, email_verified_at)
				VALUES (NULL, $1, $2, NULL, 'user', now())
				RETURNING id`,
				in.Email, displayName,
			).Scan(&userID); err != nil {
				return nil, err
			}
			res.WasCreated = true
		} else {
			res.WasLinked = true
		}
	}

	// Ensure membership in the SSO-config tenant.
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_tenant_memberships (user_id, tenant_id, role)
		VALUES ($1, $2, 'user')
		ON CONFLICT (user_id, tenant_id) DO NOTHING`,
		userID, in.TenantID,
	); err != nil {
		return nil, err
	}

	// Upsert the identity row.
	var (
		identID                       uuid.UUID
		identUser                     uuid.UUID
		identKind, identIss, identSub string
		identEmail, identClaims       string
		firstSeen, lastSeen           time.Time
	)
	if err := tx.QueryRow(ctx, `
		INSERT INTO user_sso_identities
		    (user_id, provider_kind, issuer, subject, email, raw_claims)
		VALUES ($1,$2,$3,$4,NULLIF($5,'')::citext,$6::jsonb)
		ON CONFLICT (provider_kind, issuer, subject) DO UPDATE SET
		    user_id      = EXCLUDED.user_id,
		    email        = EXCLUDED.email,
		    raw_claims   = EXCLUDED.raw_claims,
		    last_used_at = now()
		RETURNING id, user_id, provider_kind, issuer, subject,
		          COALESCE(email::text,''), raw_claims::text,
		          first_seen_at, last_used_at`,
		userID, in.ProviderKind, in.Issuer, in.Subject, in.Email, string(rawClaims),
	).Scan(
		&identID, &identUser, &identKind, &identIss, &identSub,
		&identEmail, &identClaims, &firstSeen, &lastSeen,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	u, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	res.User = u
	res.Identity = &UserSSOIdentity{
		ID: identID, UserID: identUser, ProviderKind: identKind,
		Issuer: identIss, Subject: identSub, Email: identEmail,
		RawClaims: json.RawMessage(identClaims),
		FirstSeenAt: firstSeen, LastUsedAt: lastSeen,
	}
	return res, nil
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

var ErrLastAuthFactor = errors.New("cannot unlink: this is the only way to sign in; set a password first")
