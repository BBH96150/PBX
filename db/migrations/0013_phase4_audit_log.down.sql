BEGIN;
ALTER TABLE tenants DROP COLUMN IF EXISTS require_email_verified;
DROP TABLE IF EXISTS audit_log;
DELETE FROM schema_meta WHERE key='migration' AND value='0013_phase4_audit_log';
COMMIT;
