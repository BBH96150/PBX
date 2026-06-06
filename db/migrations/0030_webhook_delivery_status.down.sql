BEGIN;
ALTER TABLE webhook_endpoints
    DROP COLUMN IF EXISTS last_status,
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS last_attempt_at;
COMMIT;
