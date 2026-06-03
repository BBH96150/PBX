BEGIN;
ALTER TABLE dids
    DROP COLUMN IF EXISTS schedule_id,
    DROP COLUMN IF EXISTS closed_destination_kind,
    DROP COLUMN IF EXISTS closed_destination_id;
DROP TABLE IF EXISTS business_schedule_holidays;
DROP TABLE IF EXISTS business_schedule_periods;
DROP TABLE IF EXISTS business_schedules;
INSERT INTO schema_meta(key, value) VALUES ('migration','0025_phaseA_tenant_moh')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
COMMIT;
