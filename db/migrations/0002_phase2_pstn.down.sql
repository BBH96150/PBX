BEGIN;

ALTER TABLE dids DROP COLUMN IF EXISTS carrier_account_id;
DROP TABLE IF EXISTS carrier_accounts;

-- Intentionally do NOT delete the seeded CallCentric carrier row:
-- dids.carrier_id (from 0001) may still reference it. The 0001 down migration
-- drops the carriers table itself if a full rollback is needed.

UPDATE schema_meta SET value = '0001_init' WHERE key = 'migration';

COMMIT;
