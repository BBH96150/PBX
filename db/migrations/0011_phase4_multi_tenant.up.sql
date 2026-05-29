-- Phase 4.4: enterprise auth — multi-tenant memberships + billing fields.
--
-- The model:
--   - users.tenant_id stays for backwards-compat but is deprecated. Going
--     forward, membership lives in user_tenant_memberships.
--   - One user can belong to many tenants, each with its own role.
--   - users.role keeps only "super_admin" semantics (NULL tenant_id, no
--     memberships). All tenant-scoped roles ('user', 'tenant_admin') are
--     stored on the membership row.
--   - Existing users get backfilled — each row with non-NULL tenant_id
--     gets a matching membership.

BEGIN;

-- Tenant billing / plan fields ---------------------------------------------
ALTER TABLE tenants
    ADD COLUMN plan          TEXT NOT NULL DEFAULT 'trial'
                              CHECK (plan IN ('trial','starter','pro','enterprise')),
    ADD COLUMN billing_email CITEXT,
    ADD COLUMN billing_phone TEXT,
    ADD COLUMN trial_ends_at TIMESTAMPTZ;

-- Membership join table ----------------------------------------------------
CREATE TABLE user_tenant_memberships (
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    role       TEXT NOT NULL DEFAULT 'user'
                CHECK (role IN ('user','tenant_admin')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, tenant_id)
);
CREATE INDEX user_tenant_memberships_tenant ON user_tenant_memberships(tenant_id);

-- Backfill: every existing user with a tenant_id becomes a tenant_admin
-- of that tenant (we have no granular history to do better). Super-admins
-- (NULL tenant_id) get no rows here — they're not tenant-scoped.
INSERT INTO user_tenant_memberships (user_id, tenant_id, role)
SELECT id, tenant_id, 'tenant_admin'
  FROM users WHERE tenant_id IS NOT NULL
ON CONFLICT DO NOTHING;

-- Add a status to memberships for invite flow (future): for now everyone is active.
-- Skipping invites table this wave; comes with /v1/invites endpoints.

INSERT INTO schema_meta(key, value) VALUES ('migration','0011_phase4_multi_tenant')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
