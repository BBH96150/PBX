BEGIN;

DROP TABLE IF EXISTS password_reset_tokens CASCADE;
DROP TABLE IF EXISTS user_invites CASCADE;
ALTER TABLE users DROP COLUMN IF EXISTS email_verified_at;

UPDATE schema_meta SET value = '0011_phase4_multi_tenant' WHERE key = 'migration';

COMMIT;
