-- Phase 4.10: SAML 2.0 SSO (per-tenant) — sibling of the OIDC table.
--
-- One tenant can have at most one SSO method (OIDC OR SAML) — enforced at
-- the application layer since the constraint spans two tables.
--
-- IdP metadata: stored as raw XML. Optionally re-fetched from idp_metadata_url
-- (auto-refresh is deferred — for now, admin re-pastes when their IdP rotates).
--
-- SP (service-provider) keypair is platform-wide, loaded from env
-- (SAML_SP_CERT_PEM, SAML_SP_KEY_PEM). All tenants share one SP cert; admins
-- register that cert with their IdP. Simpler key management than per-tenant.

BEGIN;

CREATE TABLE tenant_saml_configs (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          UUID NOT NULL UNIQUE REFERENCES tenants(id) ON DELETE CASCADE,
    label              TEXT NOT NULL DEFAULT '',
    idp_metadata_xml   TEXT NOT NULL,                  -- pasted by admin
    idp_metadata_url   TEXT,                            -- optional source for re-fetch
    entity_id_override TEXT,                            -- usually derived; can override SP entityID
    attr_email         TEXT NOT NULL DEFAULT 'email',   -- assertion attribute name → email
    attr_name          TEXT NOT NULL DEFAULT 'name',    -- assertion attribute name → display_name
    enabled            BOOLEAN NOT NULL DEFAULT true,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Domain mapping is shared with OIDC: tenant_sso_domains.tenant_id points
-- to whichever provider that tenant has configured. The lookup picks
-- whichever is enabled (OIDC takes precedence if both exist — guarded by
-- the app since neither schema can enforce it cross-table).

INSERT INTO schema_meta(key, value) VALUES ('migration','0017_phase4_saml')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
