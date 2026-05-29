BEGIN;
DROP TABLE IF EXISTS email_verification_tokens;
DELETE FROM schema_meta WHERE key='migration' AND value='0016_phase4_verify_email';
COMMIT;
