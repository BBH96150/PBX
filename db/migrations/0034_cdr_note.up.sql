-- Free-text note/tag on call records (sales/support workflows; flows through the read API).
BEGIN;
ALTER TABLE cdrs ADD COLUMN note TEXT;
COMMIT;
