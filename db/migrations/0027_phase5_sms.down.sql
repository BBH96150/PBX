BEGIN;
DROP TABLE IF EXISTS sms_messages;
INSERT INTO schema_meta(key, value) VALUES ('migration','0026_phaseA_business_hours')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
COMMIT;
