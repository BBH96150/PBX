BEGIN;
ALTER TABLE carriers
    ADD COLUMN default_auth_realm TEXT;
-- CallCentric: post-2022 the SIP proxy is sip.callcentric.net but the
-- auth realm is still callcentric.com. Without this distinction every
-- digest auth fails (the realm gets baked into the hash).
UPDATE carriers SET default_auth_realm = 'callcentric.com' WHERE kind = 'callcentric';
INSERT INTO schema_meta(key, value) VALUES ('migration','0022_phase5_carrier_auth_realm_default')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
COMMIT;
