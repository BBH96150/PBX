-- Phase 5.1 follow-up: let admins override the SIP proxy host per trunk.
-- Default is whatever's on the parent carrier row (e.g. "callcentric.com"),
-- but real CallCentric accounts often need "alpha.callcentric.com" or similar
-- because CallCentric load-balances accounts to specific clusters.

BEGIN;

ALTER TABLE carrier_accounts
    ADD COLUMN proxy_host_override TEXT;

INSERT INTO schema_meta(key, value) VALUES ('migration','0020_phase5_carrier_proxy_override')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
