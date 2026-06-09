-- E911 dispatchable locations (RAY BAUM's Act + Kari's Law). A tenant admin
-- defines named civic addresses; each extension can be assigned one so that when
-- a phone dials 911 the dispatchable location can be stamped onto the call (and
-- surfaced to a notification webhook). Today emergency numbers don't route at
-- all — this is the data model the dialplan's 911 handler reads from.
BEGIN;

CREATE TABLE e911_locations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    label       TEXT NOT NULL,
    street      TEXT NOT NULL,
    street2     TEXT,
    city        TEXT NOT NULL,
    region      TEXT NOT NULL,
    postal_code TEXT NOT NULL,
    country     TEXT NOT NULL DEFAULT 'US',
    enabled     BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A phone's dispatchable location. ON DELETE SET NULL so deleting a location
-- never orphans an extension row.
ALTER TABLE extensions
    ADD COLUMN e911_location_id UUID NULL REFERENCES e911_locations(id) ON DELETE SET NULL;

COMMIT;
