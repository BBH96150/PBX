-- Phase 4.3: real email + bcrypt-password login for the admin portal.
--
-- The `users` table already has password_hash + role from 0001. Two
-- small changes:
--   1. tenant_id becomes NULLable so super-admin users exist without
--      belonging to a tenant.
--   2. global email uniqueness for super-admin (NULL tenant_id) and
--      per-tenant uniqueness for the rest is enforced via partial indexes.

BEGIN;

ALTER TABLE users ALTER COLUMN tenant_id DROP NOT NULL;

-- Drop the old (tenant_id, email) UNIQUE constraint and replace with
-- two partial indexes that handle the NULL-tenant case correctly.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_tenant_id_email_key;

CREATE UNIQUE INDEX users_email_per_tenant
    ON users (tenant_id, email)
    WHERE tenant_id IS NOT NULL;

CREATE UNIQUE INDEX users_email_super_admin
    ON users (email)
    WHERE tenant_id IS NULL;

INSERT INTO schema_meta(key, value) VALUES ('migration','0010_phase4_user_auth')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
