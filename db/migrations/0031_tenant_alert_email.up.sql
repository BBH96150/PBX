-- Optional per-tenant override for where operational alerts (trunk down/up) go.
-- Resolution order at alert time: this override -> the tenant's admins ->
-- the global ALERT_EMAIL fallback.
BEGIN;
ALTER TABLE tenants ADD COLUMN alert_email TEXT;
COMMIT;
