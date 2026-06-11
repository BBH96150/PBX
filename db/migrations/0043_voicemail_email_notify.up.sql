-- Voicemail-to-email notifications: per-box opt-in. When a new voicemail is
-- recorded for an extension whose box has voicemail_email_enabled = true and a
-- voicemail_email_address set, the control-plane sends a best-effort email
-- notification (caller id, called extension, time, duration, portal link).
--
-- This is a notification feature only — there is NO change to live call routing
-- or the dialplan. The legacy `email` CITEXT column on voicemail_boxes (added in
-- 0004) is left in place; these new columns are the explicit opt-in toggle +
-- recipient the portal now edits.
BEGIN;

ALTER TABLE voicemail_boxes
    ADD COLUMN voicemail_email_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN voicemail_email_address CITEXT;  -- recipient; NULL = none

INSERT INTO schema_meta(key, value) VALUES ('migration','0043_voicemail_email_notify')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
