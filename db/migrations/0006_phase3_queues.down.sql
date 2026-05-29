BEGIN;

-- Revert ivr_options action_kind enum to the pre-queue set.
ALTER TABLE ivr_options DROP CONSTRAINT IF EXISTS ivr_options_action_kind_check;
ALTER TABLE ivr_options ADD CONSTRAINT ivr_options_action_kind_check
    CHECK (action_kind IN ('extension','ring_group','voicemail','ivr','hangup','dial_e164'));

DROP TABLE IF EXISTS queue_agents CASCADE;
DROP TABLE IF EXISTS queues CASCADE;

UPDATE schema_meta SET value = '0005_phase3_ivr' WHERE key = 'migration';

COMMIT;
