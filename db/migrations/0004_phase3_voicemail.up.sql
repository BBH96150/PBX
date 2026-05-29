-- Phase 3 Wave 2: voicemail boxes + messages.
--
-- Box-per-extension for now (1:1). Wave 2.5 may add shared/department boxes.
-- Messages reference the box (boxes can outlive individual messages).

BEGIN;

CREATE TABLE voicemail_boxes (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    extension_id          UUID NOT NULL REFERENCES extensions(id) ON DELETE CASCADE,
    pin                   TEXT NOT NULL CHECK (pin ~ '^[0-9]{4,12}$'),
    email                 CITEXT,                          -- VM-to-email; NULL = no email
    timezone              TEXT NOT NULL DEFAULT 'UTC',
    max_messages          INT  NOT NULL DEFAULT 100 CHECK (max_messages BETWEEN 1 AND 1000),
    max_msg_duration_sec  INT  NOT NULL DEFAULT 300 CHECK (max_msg_duration_sec BETWEEN 10 AND 1800),
    greeting_path         TEXT,                            -- absolute path to custom greeting on FS host
    enabled               BOOLEAN NOT NULL DEFAULT true,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (extension_id)
);
CREATE INDEX voicemail_boxes_tenant ON voicemail_boxes(tenant_id);

CREATE TRIGGER voicemail_boxes_set_updated_at
    BEFORE UPDATE ON voicemail_boxes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE voicemail_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    box_id          UUID NOT NULL REFERENCES voicemail_boxes(id) ON DELETE CASCADE,
    caller_id_num   TEXT,
    caller_id_name  TEXT,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    duration_sec    INT,
    audio_path      TEXT NOT NULL,                         -- absolute path on FS host
    status          TEXT NOT NULL DEFAULT 'new'
                      CHECK (status IN ('new','saved','deleted')),
    played_at       TIMESTAMPTZ,
    deleted_at      TIMESTAMPTZ
);
CREATE INDEX voicemail_messages_box_received
    ON voicemail_messages(box_id, received_at DESC)
    WHERE status <> 'deleted';

INSERT INTO schema_meta(key, value) VALUES ('migration','0004_phase3_voicemail')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
