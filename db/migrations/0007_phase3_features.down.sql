BEGIN;

ALTER TABLE extensions
    DROP COLUMN IF EXISTS recording_enabled,
    DROP COLUMN IF EXISTS cf_no_answer,
    DROP COLUMN IF EXISTS cf_busy,
    DROP COLUMN IF EXISTS cf_immediate,
    DROP COLUMN IF EXISTS do_not_disturb;

UPDATE schema_meta SET value = '0006_phase3_queues' WHERE key = 'migration';

COMMIT;
