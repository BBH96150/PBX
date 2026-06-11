BEGIN;

ALTER TABLE cdrs DROP COLUMN IF EXISTS disposition_code_id;
DROP TABLE IF EXISTS disposition_codes;

COMMIT;
