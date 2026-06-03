-- Business-hours / time-based routing.
--
-- A tenant defines schedules (sets of weekly open periods in a timezone). A DID
-- can reference a schedule + a "closed" destination: when the schedule is open
-- the DID routes normally (its destination_*), when closed it routes to
-- closed_destination_*. DIDs with no schedule are unaffected (NULL schedule_id).

BEGIN;

CREATE TABLE business_schedules (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    timezone    TEXT NOT NULL DEFAULT 'UTC',   -- IANA tz, e.g. 'America/New_York'
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX business_schedules_tenant ON business_schedules(tenant_id);

CREATE TABLE business_schedule_periods (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    schedule_id  UUID NOT NULL REFERENCES business_schedules(id) ON DELETE CASCADE,
    weekday      INT  NOT NULL CHECK (weekday BETWEEN 0 AND 6),  -- 0 = Sunday
    open_time    TIME NOT NULL,
    close_time   TIME NOT NULL,
    CHECK (close_time > open_time)
);
CREATE INDEX business_schedule_periods_schedule ON business_schedule_periods(schedule_id);

-- Optional date-specific overrides (holidays / one-off closures or openings).
CREATE TABLE business_schedule_holidays (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    schedule_id  UUID NOT NULL REFERENCES business_schedules(id) ON DELETE CASCADE,
    on_date      DATE NOT NULL,
    name         TEXT,
    is_open      BOOLEAN NOT NULL DEFAULT false,  -- false = closed all day (typical holiday)
    UNIQUE (schedule_id, on_date)
);

-- DID time-based routing: schedule + the destination used when closed.
ALTER TABLE dids
    ADD COLUMN schedule_id              UUID REFERENCES business_schedules(id) ON DELETE SET NULL,
    ADD COLUMN closed_destination_kind  TEXT
        CHECK (closed_destination_kind IS NULL OR
               closed_destination_kind IN ('extension','ivr','queue','ring_group','voicemail')),
    ADD COLUMN closed_destination_id    UUID;

INSERT INTO schema_meta(key, value) VALUES ('migration','0026_phaseA_business_hours')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
