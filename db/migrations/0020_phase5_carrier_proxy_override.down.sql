BEGIN;
ALTER TABLE carrier_accounts DROP COLUMN IF EXISTS proxy_host_override;
DELETE FROM schema_meta WHERE key='migration' AND value='0020_phase5_carrier_proxy_override';
COMMIT;
