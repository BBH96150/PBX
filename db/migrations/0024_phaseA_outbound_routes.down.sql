-- Reverse Phase A outbound routing.
BEGIN;

DROP TABLE IF EXISTS outbound_routes;

INSERT INTO schema_meta(key, value) VALUES ('migration','0023_phaseA_wildcard_sip_domain')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
