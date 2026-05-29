BEGIN;

DROP TABLE IF EXISTS user_tenant_memberships CASCADE;
ALTER TABLE tenants
    DROP COLUMN IF EXISTS trial_ends_at,
    DROP COLUMN IF EXISTS billing_phone,
    DROP COLUMN IF EXISTS billing_email,
    DROP COLUMN IF EXISTS plan;

UPDATE schema_meta SET value = '0010_phase4_user_auth' WHERE key = 'migration';

COMMIT;
