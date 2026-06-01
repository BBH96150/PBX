-- Reverse Phase A outbound routing: drop the new table and restore the
-- original (unused) 0001_init scaffold so the schema matches the 0023 state.
BEGIN;

DROP TABLE IF EXISTS outbound_routes;

CREATE TABLE outbound_routes (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    pattern             TEXT NOT NULL,            -- E.164 prefix; e.g. '+1', '+44'
    carrier_id          UUID NOT NULL REFERENCES carriers(id),
    priority            INT NOT NULL DEFAULT 100, -- lower = preferred
    caller_id_override  TEXT,
    enabled             BOOLEAN NOT NULL DEFAULT true,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX outbound_routes_tenant ON outbound_routes(tenant_id, priority);

INSERT INTO schema_meta(key, value) VALUES ('migration','0023_phaseA_wildcard_sip_domain')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
