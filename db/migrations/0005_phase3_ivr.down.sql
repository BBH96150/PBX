BEGIN;

DROP TABLE IF EXISTS ivr_options CASCADE;
DROP TABLE IF EXISTS ivrs CASCADE;

UPDATE schema_meta SET value = '0004_phase3_voicemail' WHERE key = 'migration';

COMMIT;
