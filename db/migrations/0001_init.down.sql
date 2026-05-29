BEGIN;

DROP VIEW IF EXISTS domain;
DROP VIEW IF EXISTS subscriber;

-- CASCADE handles cross-migration FKs (e.g. carrier_accounts from 0002 if a
-- failed/skipped 0002 down left them) and avoids hand-ordering churn.
DROP TABLE IF EXISTS schema_meta CASCADE;
DROP TABLE IF EXISTS cdrs CASCADE;
DROP TABLE IF EXISTS outbound_routes CASCADE;
DROP TABLE IF EXISTS dids CASCADE;
DROP TABLE IF EXISTS carriers CASCADE;
DROP TABLE IF EXISTS device_lines CASCADE;
DROP TABLE IF EXISTS devices CASCADE;
DROP TABLE IF EXISTS extensions CASCADE;
DROP TABLE IF EXISTS users CASCADE;
DROP TABLE IF EXISTS sip_domains CASCADE;
DROP TABLE IF EXISTS tenants CASCADE;

DROP FUNCTION IF EXISTS set_updated_at();

COMMIT;
