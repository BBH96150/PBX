-- Phase 4.5: invites + password reset + email verification.
--
--   user_invites           : tenant admins invite people by email
--   password_reset_tokens  : "forgot password" flow
--   users.email_verified_at: tracked even though we don't gate login yet
--
-- Tokens are stored as bcrypt hashes. The plaintext shape is "sip_<kind>_<48hex>"
-- so we can route an incoming token to the right store at validate time
-- without trying every table.

BEGIN;

ALTER TABLE users
    ADD COLUMN email_verified_at TIMESTAMPTZ;

CREATE TABLE user_invites (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email           CITEXT NOT NULL,
    role            TEXT NOT NULL DEFAULT 'user'
                      CHECK (role IN ('user','tenant_admin')),
    token_prefix    TEXT NOT NULL,             -- first 8 hex chars of plaintext for fast lookup
    token_hash      TEXT NOT NULL,             -- bcrypt of full plaintext
    invited_by      UUID REFERENCES users(id) ON DELETE SET NULL,
    expires_at      TIMESTAMPTZ NOT NULL DEFAULT now() + INTERVAL '7 days',
    accepted_at     TIMESTAMPTZ,
    accepted_by     UUID REFERENCES users(id) ON DELETE SET NULL,
    revoked_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX user_invites_token_prefix ON user_invites(token_prefix);
CREATE INDEX user_invites_tenant ON user_invites(tenant_id);
CREATE UNIQUE INDEX user_invites_pending_per_email_per_tenant
    ON user_invites(tenant_id, email)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

CREATE TABLE password_reset_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_prefix TEXT NOT NULL,
    token_hash   TEXT NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL DEFAULT now() + INTERVAL '2 hours',
    used_at      TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX password_reset_tokens_prefix ON password_reset_tokens(token_prefix);
CREATE INDEX password_reset_tokens_user ON password_reset_tokens(user_id);

INSERT INTO schema_meta(key, value) VALUES ('migration','0012_phase4_invites_resets')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
