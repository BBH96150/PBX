BEGIN;

DROP INDEX IF EXISTS devices_rps_pending;
ALTER TABLE devices
    DROP COLUMN IF EXISTS rps_last_error,
    DROP COLUMN IF EXISTS rps_synced_at;

UPDATE schema_meta SET value = '0007_phase3_features' WHERE key = 'migration';

COMMIT;
