BEGIN;

DROP TABLE IF EXISTS api_tokens CASCADE;

UPDATE schema_meta SET value = '0008_phase3_rps_sync' WHERE key = 'migration';

COMMIT;
