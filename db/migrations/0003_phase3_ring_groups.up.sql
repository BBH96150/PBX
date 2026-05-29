-- Phase 3 Wave 1: ring/hunt groups.
--
-- Single table covers both "ring group" (simultaneous) and "hunt group"
-- (sequential) semantics via a strategy enum. Round-robin and random
-- strategies are accepted by the schema but the dialplan handler skips
-- them until Wave 1.5 (need state tracking).

BEGIN;

CREATE TABLE ring_groups (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    extension         TEXT CHECK (extension IS NULL OR extension ~ '^[0-9*#]+$'),
    name              TEXT NOT NULL,
    strategy          TEXT NOT NULL DEFAULT 'simultaneous'
                        CHECK (strategy IN ('simultaneous','sequential','round_robin','random')),
    ring_timeout_sec  INT  NOT NULL DEFAULT 30
                        CHECK (ring_timeout_sec BETWEEN 5 AND 300),
    fallback_kind     TEXT CHECK (fallback_kind IS NULL OR fallback_kind IN
                          ('extension','ring_group','voicemail','ivr','hangup')),
    fallback_id       UUID,                          -- polymorphic; app enforces
    caller_id_prefix  TEXT,                          -- e.g. "[Sales] "; prepended to caller-id-name
    enabled           BOOLEAN NOT NULL DEFAULT true,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, extension)
);
CREATE INDEX ring_groups_tenant ON ring_groups(tenant_id);

CREATE TABLE ring_group_members (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ring_group_id   UUID NOT NULL REFERENCES ring_groups(id) ON DELETE CASCADE,
    extension_id    UUID NOT NULL REFERENCES extensions(id) ON DELETE CASCADE,
    priority        INT NOT NULL DEFAULT 100,        -- lower = earlier in sequential order
    ring_delay_sec  INT NOT NULL DEFAULT 0
                      CHECK (ring_delay_sec BETWEEN 0 AND 60),
    enabled         BOOLEAN NOT NULL DEFAULT true,
    UNIQUE (ring_group_id, extension_id)
);
CREATE INDEX ring_group_members_extension ON ring_group_members(extension_id);

CREATE TRIGGER ring_groups_set_updated_at
    BEFORE UPDATE ON ring_groups
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- dids destination_kind enum: rename 'hunt_group' → 'ring_group' to match the
-- new table name. (Wave 2+ adds 'voicemail', 'queue', 'ivr' wiring; the enum
-- already lists them so no migration needed for those.)
ALTER TABLE dids DROP CONSTRAINT IF EXISTS dids_destination_kind_check;
ALTER TABLE dids ADD CONSTRAINT dids_destination_kind_check
    CHECK (destination_kind IN ('extension','ivr','queue','ring_group','voicemail'));

INSERT INTO schema_meta(key, value) VALUES ('migration','0003_phase3_ring_groups')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
