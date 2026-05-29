-- Phase 4.0: API authentication tokens.
--
-- tenant_id IS NULL → "super-admin" token; can access any tenant. Otherwise
-- the token is scoped to that tenant and the middleware rejects cross-tenant
-- URL params.
--
-- token_prefix is the first 8 hex chars after the "sip_" prefix; it lets us
-- narrow the bcrypt comparison to typically 1 candidate row even with many
-- thousands of tokens. The plaintext token is shown to the admin only once
-- on create.

BEGIN;

CREATE TABLE api_tokens (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID REFERENCES tenants(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    token_prefix  TEXT NOT NULL,                              -- e.g. "ab12cd34"
    token_hash    TEXT NOT NULL,                              -- bcrypt of full token
    scope         TEXT NOT NULL DEFAULT 'write'
                    CHECK (scope IN ('read','write','admin')),
    expires_at    TIMESTAMPTZ,
    last_used_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX api_tokens_prefix ON api_tokens(token_prefix);
CREATE INDEX api_tokens_tenant ON api_tokens(tenant_id);

INSERT INTO schema_meta(key, value) VALUES ('migration','0009_phase4_api_tokens')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
