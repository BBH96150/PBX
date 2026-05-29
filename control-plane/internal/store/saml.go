package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

type TenantSAMLConfig struct {
	ID               uuid.UUID `json:"id"`
	TenantID         uuid.UUID `json:"tenant_id"`
	Label            string    `json:"label"`
	IDPMetadataXML   string    `json:"idp_metadata_xml"`
	IDPMetadataURL   string    `json:"idp_metadata_url,omitempty"`
	EntityIDOverride string    `json:"entity_id_override,omitempty"`
	AttrEmail        string    `json:"attr_email"`
	AttrName         string    `json:"attr_name"`
	Enabled          bool      `json:"enabled"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type SaveTenantSAMLConfigInput struct {
	TenantID         uuid.UUID
	Label            string
	IDPMetadataXML   string
	IDPMetadataURL   string
	EntityIDOverride string
	AttrEmail        string
	AttrName         string
	Enabled          bool
}

func (s *Store) SaveTenantSAMLConfig(ctx context.Context, in SaveTenantSAMLConfigInput) (*TenantSAMLConfig, error) {
	if in.AttrEmail == "" {
		in.AttrEmail = "email"
	}
	if in.AttrName == "" {
		in.AttrName = "name"
	}
	const q = `
		INSERT INTO tenant_saml_configs
		    (tenant_id, label, idp_metadata_xml, idp_metadata_url,
		     entity_id_override, attr_email, attr_name, enabled)
		VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,$7,$8)
		ON CONFLICT (tenant_id) DO UPDATE SET
		    label              = EXCLUDED.label,
		    idp_metadata_xml   = EXCLUDED.idp_metadata_xml,
		    idp_metadata_url   = EXCLUDED.idp_metadata_url,
		    entity_id_override = EXCLUDED.entity_id_override,
		    attr_email         = EXCLUDED.attr_email,
		    attr_name          = EXCLUDED.attr_name,
		    enabled            = EXCLUDED.enabled,
		    updated_at         = now()
		RETURNING id, tenant_id, label, idp_metadata_xml,
		          COALESCE(idp_metadata_url,''),
		          COALESCE(entity_id_override,''),
		          attr_email, attr_name, enabled, created_at, updated_at`
	var c TenantSAMLConfig
	err := s.DB.QueryRow(ctx, q,
		in.TenantID, in.Label, in.IDPMetadataXML, in.IDPMetadataURL,
		in.EntityIDOverride, in.AttrEmail, in.AttrName, in.Enabled,
	).Scan(
		&c.ID, &c.TenantID, &c.Label, &c.IDPMetadataXML, &c.IDPMetadataURL,
		&c.EntityIDOverride, &c.AttrEmail, &c.AttrName, &c.Enabled,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) GetTenantSAMLConfig(ctx context.Context, tenantID uuid.UUID) (*TenantSAMLConfig, error) {
	const q = `
		SELECT id, tenant_id, label, idp_metadata_xml,
		       COALESCE(idp_metadata_url,''),
		       COALESCE(entity_id_override,''),
		       attr_email, attr_name, enabled, created_at, updated_at
		  FROM tenant_saml_configs WHERE tenant_id = $1`
	var c TenantSAMLConfig
	err := s.DB.QueryRow(ctx, q, tenantID).Scan(
		&c.ID, &c.TenantID, &c.Label, &c.IDPMetadataXML, &c.IDPMetadataURL,
		&c.EntityIDOverride, &c.AttrEmail, &c.AttrName, &c.Enabled,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) DisableTenantSAMLConfig(ctx context.Context, tenantID uuid.UUID) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE tenant_saml_configs SET enabled = false, updated_at = now() WHERE tenant_id = $1`,
		tenantID)
	return err
}

// LookupSAMLByEmailDomain returns the tenant + SAML config for an email
// domain. Mirrors LookupSSOByEmailDomain but for the SAML table.
func (s *Store) LookupSAMLByEmailDomain(ctx context.Context, email string) (*Tenant, *TenantSAMLConfig, error) {
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
		       c.id, c.tenant_id, c.label, c.idp_metadata_xml,
		       COALESCE(c.idp_metadata_url,''),
		       COALESCE(c.entity_id_override,''),
		       c.attr_email, c.attr_name, c.enabled, c.created_at, c.updated_at
		  FROM tenant_sso_domains d
		  JOIN tenants t ON t.id = d.tenant_id
		  JOIN tenant_saml_configs c ON c.tenant_id = d.tenant_id
		 WHERE d.domain = $1::citext AND c.enabled = true
		 LIMIT 1`
	var t Tenant
	var c TenantSAMLConfig
	err := s.DB.QueryRow(ctx, q, domain).Scan(
		&t.ID, &t.Slug, &t.Name, &t.Status, &t.Plan,
		&t.BillingEmail, &t.BillingPhone, &t.TrialEndsAt,
		&t.CreatedAt, &t.UpdatedAt,
		&c.ID, &c.TenantID, &c.Label, &c.IDPMetadataXML, &c.IDPMetadataURL,
		&c.EntityIDOverride, &c.AttrEmail, &c.AttrName, &c.Enabled,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, nil, nil
	}
	return &t, &c, nil
}

// HasSSOConfigured returns true if the tenant has either an OIDC or SAML
// config (enabled or not). Used by the admin pages to prevent configuring
// both simultaneously.
func (s *Store) HasSSOConfigured(ctx context.Context, tenantID uuid.UUID, excludeKind string) (bool, error) {
	if excludeKind != "oidc" {
		var n int
		if err := s.DB.QueryRow(ctx,
			`SELECT COUNT(*) FROM tenant_sso_configs WHERE tenant_id = $1 AND enabled = true`,
			tenantID).Scan(&n); err != nil {
			return false, err
		}
		if n > 0 {
			return true, nil
		}
	}
	if excludeKind != "saml" {
		var n int
		if err := s.DB.QueryRow(ctx,
			`SELECT COUNT(*) FROM tenant_saml_configs WHERE tenant_id = $1 AND enabled = true`,
			tenantID).Scan(&n); err != nil {
			return false, err
		}
		if n > 0 {
			return true, nil
		}
	}
	return false, nil
}

var ErrSSOAlreadyConfigured = errors.New("this tenant already has an SSO provider configured — disable it before configuring another")
