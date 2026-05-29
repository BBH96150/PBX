BEGIN;

DROP TABLE IF EXISTS ring_group_members CASCADE;
DROP TABLE IF EXISTS ring_groups CASCADE;

ALTER TABLE dids DROP CONSTRAINT IF EXISTS dids_destination_kind_check;
ALTER TABLE dids ADD CONSTRAINT dids_destination_kind_check
    CHECK (destination_kind IN ('extension','ivr','queue','hunt_group','voicemail'));

UPDATE schema_meta SET value = '0002_phase2_pstn' WHERE key = 'migration';

COMMIT;
