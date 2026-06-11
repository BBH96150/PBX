BEGIN;

ALTER TABLE voicemail_boxes
    DROP COLUMN IF EXISTS voicemail_email_enabled,
    DROP COLUMN IF EXISTS voicemail_email_address;

INSERT INTO schema_meta(key, value) VALUES ('migration','0004_phase3_voicemail')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
