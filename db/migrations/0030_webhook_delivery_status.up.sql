-- Track each webhook endpoint's most recent delivery outcome so operators can
-- see at a glance whether it's healthy. Bounded (one row per endpoint) — no
-- unbounded delivery-log table.
BEGIN;
ALTER TABLE webhook_endpoints
    ADD COLUMN last_status      TEXT,          -- 'ok' | 'fail'
    ADD COLUMN last_error       TEXT,
    ADD COLUMN last_attempt_at  TIMESTAMPTZ;
COMMIT;
