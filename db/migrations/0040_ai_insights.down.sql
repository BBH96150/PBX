BEGIN;

ALTER TABLE voicemail_messages DROP COLUMN IF EXISTS transcript;
DROP TABLE IF EXISTS call_insights;

COMMIT;
