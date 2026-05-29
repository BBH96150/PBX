BEGIN;
ALTER TABLE carriers DROP COLUMN IF EXISTS default_auth_realm;
DELETE FROM schema_meta WHERE key='migration' AND value='0022_phase5_carrier_auth_realm_default';
COMMIT;
