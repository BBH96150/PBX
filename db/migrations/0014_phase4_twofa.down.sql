BEGIN;
ALTER TABLE tenants DROP COLUMN IF EXISTS require_2fa;
DROP TABLE IF EXISTS user_trusted_devices;
DROP TABLE IF EXISTS user_2fa_recovery_codes;
DROP TABLE IF EXISTS user_2fa_methods;
DELETE FROM schema_meta WHERE key='migration' AND value='0014_phase4_twofa';
COMMIT;
