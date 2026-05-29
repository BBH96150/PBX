-- Phase 5.0: WebRTC browser softphone — per-user webphone identity layered
-- on top of an existing extension.
--
-- Design:
--   - The desk-phone identity (extensions.sip_username + ha1) stays untouched
--     so users keep their physical phone registration when they open the portal.
--   - A *separate* SIP identity per extension, derived as <ext>-wp, gets its
--     own HA1. The browser SIP.js registers using this second identity.
--   - Both identities live behind the same extension number; Kamailio's
--     usrloc happily holds multiple contacts per AOR, so inbound calls to
--     1001 ring the desk phone AND the browser tab.
--   - Credentials are rotated on every "Open softphone" click. Plaintext
--     is returned once, HA1 stored persistently.

BEGIN;

ALTER TABLE extensions
    ADD COLUMN webphone_enabled    BOOLEAN     NOT NULL DEFAULT false,
    ADD COLUMN webphone_username   TEXT,                       -- usually <sip_username>-wp
    ADD COLUMN webphone_ha1        TEXT,                       -- MD5(user:realm:password)
    ADD COLUMN webphone_ha1b       TEXT,
    ADD COLUMN webphone_rotated_at TIMESTAMPTZ;

-- Drop + recreate subscriber view to include webphone identities as a second
-- row per extension. UNION ALL keeps it cheap.
DROP VIEW IF EXISTS subscriber;

CREATE VIEW subscriber AS
-- Primary (desk-phone / softclient with provisioned creds) identity
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
WHERE e.status = 'active'
UNION ALL
-- Phase 5.0 webphone identity (browser SIP.js)
SELECT
    (e.id::text || ':wp')        AS id,
    e.webphone_username          AS username,
    d.domain                     AS domain,
    ''::text                     AS password,
    ''::text                     AS email_address,
    e.webphone_ha1               AS ha1,
    e.webphone_ha1b              AS ha1b,
    NULL::text                   AS rpid
FROM extensions e
JOIN sip_domains d ON d.id = e.sip_domain_id
WHERE e.status = 'active'
  AND e.webphone_enabled = true
  AND e.webphone_username IS NOT NULL
  AND e.webphone_ha1 IS NOT NULL;

INSERT INTO schema_meta(key, value) VALUES ('migration','0018_phase5_webphone')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
