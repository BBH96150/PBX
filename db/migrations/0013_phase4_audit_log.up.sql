-- Phase 4.6: append-only audit log + email verification gate.
--
--   audit_log              : security/compliance trail of authenticated and
--                            unauthenticated security-relevant events
--   users.email_verified_at: existed since 0012; enforcement (login gate) is
--                            opt-in per-tenant via tenants.require_email_verified
--
-- Design notes:
--   - All columns nullable except event + created_at so failed-login records
--     (no actor_user_id, no token_id) can still be persisted.
--   - tenant_id NULL = platform-level event (e.g. super-admin actions).
--   - payload JSONB carries event-specific context. Keep PII out of it where
--     reasonable — store IDs, not raw passwords.
--   - Index covers the common queries: per-tenant timeline, per-actor lookup,
--     by-event filtering for security reviews.

BEGIN;

CREATE TABLE audit_log (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID REFERENCES tenants(id) ON DELETE SET NULL,
    actor_user_id   UUID REFERENCES users(id) ON DELETE SET NULL,
    actor_token_id  UUID REFERENCES api_tokens(id) ON DELETE SET NULL,
    actor_email     TEXT,                -- snapshot for failed-login + post-deletion lookback
    event           TEXT NOT NULL,       -- e.g. "auth.login.success"
    target_type     TEXT,                -- e.g. "user","tenant","invite","api_token"
    target_id       UUID,                -- subject of the action when applicable
    ip_address      INET,
    user_agent      TEXT,
    payload         JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_tenant_time    ON audit_log (tenant_id, created_at DESC);
CREATE INDEX audit_log_actor_time     ON audit_log (actor_user_id, created_at DESC);
CREATE INDEX audit_log_event_time     ON audit_log (event, created_at DESC);
CREATE INDEX audit_log_actor_email    ON audit_log (actor_email)
    WHERE actor_user_id IS NULL;

-- Per-tenant policy: require verified email before login is allowed.
-- Defaults off so existing tenants don't get locked out.
ALTER TABLE tenants
    ADD COLUMN require_email_verified BOOLEAN NOT NULL DEFAULT false;

INSERT INTO schema_meta(key, value) VALUES ('migration','0013_phase4_audit_log')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
