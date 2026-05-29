-- Phase 3 #10: track RPS (manufacturer redirection) sync state per device.
--
-- Each device gets pushed to its vendor's cloud (Polycom ZTP, Yealink RPS,
-- Grandstream GDMS, etc.) so that when the phone first boots, the vendor
-- redirects it to our provisioning URL — no factory-reset + URL-entry needed.

BEGIN;

ALTER TABLE devices
    ADD COLUMN rps_synced_at  TIMESTAMPTZ,
    ADD COLUMN rps_last_error TEXT;

CREATE INDEX devices_rps_pending
    ON devices(updated_at)
    WHERE rps_synced_at IS NULL AND rps_last_error IS NULL;

INSERT INTO schema_meta(key, value) VALUES ('migration','0008_phase3_rps_sync')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
