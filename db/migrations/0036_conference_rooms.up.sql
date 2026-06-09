-- Meet-me conference bridges: dial-in audio conference rooms. A tenant admin
-- creates a room with a dialable `extension` (room number); anyone who dials it
-- is prompted for the optional member/moderator PIN and joined into a
-- FreeSWITCH conference (the static `default` profile in conference.conf.xml).
BEGIN;

CREATE TABLE conference_rooms (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    extension      TEXT NOT NULL CHECK (extension ~ '^[0-9*#]+$'),
    name           TEXT NOT NULL,
    pin            TEXT,
    moderator_pin  TEXT,
    max_members    INT NOT NULL DEFAULT 0,
    record         BOOLEAN NOT NULL DEFAULT false,
    announce_count BOOLEAN NOT NULL DEFAULT true,
    enabled        BOOLEAN NOT NULL DEFAULT true,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, extension)
);

COMMIT;
