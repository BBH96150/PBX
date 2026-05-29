BEGIN;
DROP TABLE IF EXISTS tenant_saml_configs;
DELETE FROM schema_meta WHERE key='migration' AND value='0017_phase4_saml';
COMMIT;
