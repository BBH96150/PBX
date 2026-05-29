BEGIN;
DROP INDEX IF EXISTS carrier_accounts_tenant;
ALTER TABLE carrier_accounts DROP COLUMN IF EXISTS tenant_id;
DELETE FROM schema_meta WHERE key='migration' AND value='0019_phase5_tenant_carriers';
COMMIT;
