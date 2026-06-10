-- Inbound number blocking / call screening. A tenant admin maintains a
-- blocklist of caller numbers; inbound PSTN calls whose caller matches a blocked
-- number are rejected (CALL_REJECTED) in the dialplan before they ring anyone.
-- Greenfield + immediately functional: the dialplan screens against this table
-- on every inbound PSTN call (fail-open if the lookup errors).
BEGIN;

CREATE TABLE blocked_numbers (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    number      TEXT NOT NULL,
    label       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, number)
);

CREATE INDEX idx_blocked_numbers_tenant ON blocked_numbers (tenant_id);

COMMIT;
