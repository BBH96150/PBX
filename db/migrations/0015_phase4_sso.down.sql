BEGIN;
ALTER TABLE tenants DROP COLUMN IF EXISTS require_sso;
DROP TABLE IF EXISTS user_sso_identities;
DROP TABLE IF EXISTS tenant_sso_domains;
DROP TABLE IF EXISTS tenant_sso_configs;
DELETE FROM schema_meta WHERE key='migration' AND value='0015_phase4_sso';
COMMIT;
