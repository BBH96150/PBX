-- Phase A: per-tenant outbound routing.
--
-- Replaces the "first enabled carrier_account" hack (store.PickPrimary*)
-- with explicit, longest-prefix outbound routes: a dialed E.164 is matched
-- against each route's match_prefix; the most specific enabled route wins and
-- selects which trunk carries the call (and, optionally, the caller ID).
--
-- Backwards compat: a tenant with no outbound_routes rows keeps the old
-- behavior (the dialplan falls back to PickPrimaryCarrierAccountForTenant).

BEGIN;

CREATE TABLE outbound_routes (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name                TEXT NOT NULL,
    -- E.164 prefix (leading +, digits only) the dialed number must start with.
    -- '' is the catch-all (matches every number).
    match_prefix        TEXT NOT NULL DEFAULT ''
                          CHECK (match_prefix = '' OR match_prefix ~ '^\+[0-9]+$'),
    carrier_account_id  UUID NOT NULL REFERENCES carrier_accounts(id) ON DELETE CASCADE,
    -- Optional caller-ID override (E.164). NULL = use the trunk's main DID.
    caller_id_e164      TEXT CHECK (caller_id_e164 IS NULL OR caller_id_e164 ~ '^\+[0-9]+$'),
    priority            INT NOT NULL DEFAULT 100,   -- lower = preferred on prefix-length ties
    enabled             BOOLEAN NOT NULL DEFAULT true,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX outbound_routes_match
    ON outbound_routes(tenant_id, enabled, priority);

INSERT INTO schema_meta(key, value) VALUES ('migration','0024_phaseA_outbound_routes')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
