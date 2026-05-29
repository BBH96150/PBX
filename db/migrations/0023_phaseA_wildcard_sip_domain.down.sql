-- Reverse-direction migration for Phase A.1 wildcard SIP domain rename.
-- There is no clean way to "unrename" since we'd need to know what the
-- original domain was per tenant. The historical values for the two
-- tenants in prod at the time this shipped:
--   acme → acme.sip.local
--   bbh  → sip.tendpos.com (was bbh.sip.bigbluehospitality.com originally)
-- If you need to roll back, restore from the daily Postgres backup
-- (/var/backups/postgres/) rather than running this script.

-- No-op intentionally.
SELECT 1;
