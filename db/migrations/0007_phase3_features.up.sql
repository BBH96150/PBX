-- Phase 3 Wave 5.0: per-extension feature flags.
-- DND, call-forwarding targets, always-record toggle.

BEGIN;

ALTER TABLE extensions
    ADD COLUMN do_not_disturb    BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN cf_immediate      TEXT,                  -- dialstring (digits or +E.164)
    ADD COLUMN cf_busy           TEXT,                  -- Wave 5.5 honors this (needs bridge-result branching)
    ADD COLUMN cf_no_answer      TEXT,
    ADD COLUMN recording_enabled BOOLEAN NOT NULL DEFAULT false;

INSERT INTO schema_meta(key, value) VALUES ('migration','0007_phase3_features')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
