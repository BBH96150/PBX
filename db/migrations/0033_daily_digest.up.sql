-- Opt-in daily call-summary email digest per tenant. Off by default; only
-- tenants that enable it receive email. last_digest_on dedups across restarts.
BEGIN;
ALTER TABLE tenants ADD COLUMN daily_digest BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE tenants ADD COLUMN last_digest_on DATE;
COMMIT;
