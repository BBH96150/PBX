BEGIN;

DROP TABLE IF EXISTS voicemail_messages CASCADE;
DROP TABLE IF EXISTS voicemail_boxes CASCADE;

UPDATE schema_meta SET value = '0003_phase3_ring_groups' WHERE key = 'migration';

COMMIT;
