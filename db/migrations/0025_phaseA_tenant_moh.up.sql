-- Per-tenant Music on Hold.
--
-- A tenant can set a custom hold-music source (a FreeSWITCH-resolvable URI:
-- local_stream://<dir>, shout://<url> for internet radio, or a file path/URL).
-- The dialplan sets it as the `hold_music` channel variable so transfers/holds
-- use the tenant's audio. NULL = the platform default (local_stream://moh).

BEGIN;

ALTER TABLE tenants ADD COLUMN moh_url TEXT;

INSERT INTO schema_meta(key, value) VALUES ('migration','0025_phaseA_tenant_moh')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
