BEGIN;
DROP VIEW IF EXISTS subscriber;
ALTER TABLE extensions
    DROP COLUMN IF EXISTS webphone_rotated_at,
    DROP COLUMN IF EXISTS webphone_ha1b,
    DROP COLUMN IF EXISTS webphone_ha1,
    DROP COLUMN IF EXISTS webphone_username,
    DROP COLUMN IF EXISTS webphone_enabled;
-- Restore prior view from migration 0001
CREATE VIEW subscriber AS
SELECT
    e.id::text                   AS id,
    e.sip_username               AS username,
    d.domain                     AS domain,
    e.sip_password               AS password,
    ''::text                     AS email_address,
    e.ha1                        AS ha1,
    e.ha1b                       AS ha1b,
    NULL::text                   AS rpid
FROM extensions e
JOIN sip_domains d ON d.id = e.sip_domain_id
WHERE e.status = 'active';
DELETE FROM schema_meta WHERE key='migration' AND value='0018_phase5_webphone';
COMMIT;
