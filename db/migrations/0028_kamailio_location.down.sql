-- Revert usrloc DB persistence. NOTE: only safe once Kamailio is back on
-- in-memory usrloc (WITH_USRLOCDB 0) — dropping this table while db_mode=1 is
-- live would break registration.
BEGIN;
DROP TABLE IF EXISTS location;
UPDATE version SET table_version = 1008 WHERE table_name = 'location';
COMMIT;
