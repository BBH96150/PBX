BEGIN;

DROP INDEX IF EXISTS users_email_super_admin;
DROP INDEX IF EXISTS users_email_per_tenant;

-- Removing rows that would violate the restored NOT NULL.
DELETE FROM users WHERE tenant_id IS NULL;
ALTER TABLE users ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE users ADD CONSTRAINT users_tenant_id_email_key UNIQUE (tenant_id, email);

UPDATE schema_meta SET value = '0009_phase4_api_tokens' WHERE key = 'migration';

COMMIT;
