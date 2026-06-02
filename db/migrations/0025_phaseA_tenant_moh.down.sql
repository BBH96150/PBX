BEGIN;
ALTER TABLE tenants DROP COLUMN IF EXISTS moh_url;
INSERT INTO schema_meta(key, value) VALUES ('migration','0024_phaseA_outbound_routes')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
COMMIT;
