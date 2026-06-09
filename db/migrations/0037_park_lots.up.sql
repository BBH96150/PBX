-- Call park lots: park an in-progress call to a numbered "orbit" slot so it can
-- be retrieved from any phone. A tenant admin configures a feature code to park
-- (e.g. *68) and a slot range (e.g. 700-779). Blind-transferring a call to the
-- feature code parks it (FreeSWITCH mod_valet_parking auto-assigns + announces
-- the slot); dialing the assigned slot number retrieves the parked call.
BEGIN;

CREATE TABLE park_lots (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    feature_code TEXT NOT NULL CHECK (feature_code ~ '^[0-9*#]+$'),
    slot_start   INT NOT NULL,
    slot_end     INT NOT NULL,
    enabled      BOOLEAN NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, feature_code),
    CHECK (slot_end >= slot_start)
);

COMMIT;
