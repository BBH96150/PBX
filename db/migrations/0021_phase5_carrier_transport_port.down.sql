BEGIN;
ALTER TABLE carrier_accounts
    DROP COLUMN IF EXISTS transport_override,
    DROP COLUMN IF EXISTS proxy_port_override;
DELETE FROM schema_meta WHERE key='migration' AND value='0021_phase5_carrier_transport_port';
COMMIT;
