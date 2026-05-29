-- Phase 4.8: OIDC SSO (per-tenant) + JIT user provisioning.
--
--   tenant_sso_configs  : one OIDC IdP per tenant
--   tenant_sso_domains  : email-domain → tenant mapping for login-page discovery
--   user_sso_identities : (provider_kind, issuer, subject) → local user
--   tenants.require_sso : when true, password login is blocked for members
--
-- Client secret stored AES-GCM-sealed with the same TOTP_ENCRYPTION_KEY env
-- key used for 2FA secrets.

BEGIN;

CREATE TABLE tenant_sso_configs (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                UUID NOT NULL UNIQUE REFERENCES tenants(id) ON DELETE CASCADE,
    provider_kind            TEXT NOT NULL DEFAULT 'oidc'
                                CHECK (provider_kind IN ('oidc')),
    label                    TEXT NOT NULL DEFAULT '',
    issuer_url               TEXT NOT NULL,
    client_id                TEXT NOT NULL,
    client_secret_ciphertext BYTEA NOT NULL,
    client_secret_nonce      BYTEA NOT NULL,
    scopes                   TEXT NOT NULL DEFAULT 'openid email profile',
    enabled                  BOOLEAN NOT NULL DEFAULT true,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE tenant_sso_domains (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    domain      CITEXT NOT NULL UNIQUE,         -- one tenant per domain platform-wide
    verified_at TIMESTAMPTZ,                    -- DNS TXT verification — deferred to a later wave
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX tenant_sso_domains_tenant ON tenant_sso_domains(tenant_id);

CREATE TABLE user_sso_identities (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider_kind TEXT NOT NULL DEFAULT 'oidc',
    issuer        TEXT NOT NULL,
    subject       TEXT NOT NULL,                -- IdP `sub` claim
    email         CITEXT,                       -- snapshot at first/last login
    raw_claims    JSONB NOT NULL DEFAULT '{}'::jsonb,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider_kind, issuer, subject)
);
CREATE INDEX user_sso_identities_user ON user_sso_identities(user_id);

ALTER TABLE tenants
    ADD COLUMN require_sso BOOLEAN NOT NULL DEFAULT false;

INSERT INTO schema_meta(key, value) VALUES ('migration','0015_phase4_sso')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
