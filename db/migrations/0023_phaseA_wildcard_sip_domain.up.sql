-- Phase A.1 — wildcard SIP domain pattern.
--
-- Rename every existing tenant's primary sip_domain to <slug>.pbx.tendpos.com
-- and recompute extension ha1 / ha1b hashes against the new realm so any
-- registered devices keep working after a re-register.
--
-- Notes:
--   - Hardcoding "pbx.tendpos.com" as the suffix here is intentional. The
--     control-plane reads SIP_DOMAIN_SUFFIX from env at runtime for NEW
--     tenants; this migration is the one-shot for the existing ones.
--   - extensions.sip_password is stored cleartext (no encryption wave yet),
--     so we can deterministically recompute MD5(user:realm:password). When
--     that changes, this migration moves to a Go-side helper.
--   - extensions.webphone_ha1 / ha1b are NULL when webphone_enabled=false,
--     so the COALESCE pattern leaves them alone.

-- 1. Rename every tenant's primary sip_domain.
UPDATE sip_domains sd
   SET domain = t.slug || '.pbx.tendpos.com'
  FROM tenants t
 WHERE sd.tenant_id = t.id
   AND sd.is_primary = true
   AND sd.domain <> t.slug || '.pbx.tendpos.com';

-- 2. Recompute ha1 / ha1b for every extension whose sip_domain just moved.
UPDATE extensions e
   SET ha1  = md5(e.sip_username || ':' || sd.domain || ':' || e.sip_password),
       ha1b = md5(e.sip_username || '@' || sd.domain || ':' || sd.domain || ':' || e.sip_password),
       updated_at = now()
  FROM sip_domains sd
 WHERE e.sip_domain_id = sd.id;

-- 3. Webphone creds invalidate on domain change — webphone_ha1 is already
--    an MD5 of (user:realm:password) and we don't store the plaintext, so
--    we can't recompute against the new realm. NULL them out; the next
--    /admin/softphone visit will mint fresh ones.
UPDATE extensions
   SET webphone_username   = NULL,
       webphone_ha1        = NULL,
       webphone_ha1b       = NULL,
       webphone_rotated_at = NULL,
       webphone_enabled    = false
 WHERE webphone_enabled = true;
