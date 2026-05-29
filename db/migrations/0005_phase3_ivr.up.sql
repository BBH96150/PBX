-- Phase 3 Wave 3: IVR (auto-attendant) menus.
--
-- Mapped onto FreeSWITCH's mod_ivr menus, served dynamically via
-- mod_xml_curl's `configuration` binding (ivr.conf section). The control
-- plane returns one <menu name="<ivr.id>"> per enabled IVR.
--
-- Wave 3.0 honors action_kind in {'extension','hangup'} from the dialplan;
-- the others ('ring_group','voicemail','ivr','dial_e164') are accepted by
-- the schema and admin API for forward-compat but render as hangup until
-- Wave 3.5 wires them.

BEGIN;

CREATE TABLE ivrs (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id               UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name                    TEXT NOT NULL,
    extension               TEXT CHECK (extension IS NULL OR extension ~ '^[0-9*#]+$'),
    -- Audio prompts. Defaults point at the FreeSWITCH bundled "Callie" voice
    -- pack (mounted by the safarov/freeswitch image at /usr/share/freeswitch/sounds).
    -- Phase 3.5+: support per-tenant uploaded files (absolute paths).
    greeting_long           TEXT NOT NULL DEFAULT 'ivr/ivr-welcome.wav',
    greeting_short          TEXT NOT NULL DEFAULT 'ivr/ivr-welcome_to_freeswitch.wav',
    invalid_sound           TEXT NOT NULL DEFAULT 'ivr/ivr-that_was_an_invalid_entry.wav',
    exit_sound              TEXT NOT NULL DEFAULT 'voicemail/vm-goodbye.wav',
    -- Behavior
    timeout_ms              INT  NOT NULL DEFAULT 5000
                              CHECK (timeout_ms BETWEEN 1000 AND 60000),
    inter_digit_timeout_ms  INT  NOT NULL DEFAULT 2000
                              CHECK (inter_digit_timeout_ms BETWEEN 500 AND 10000),
    max_failures            INT  NOT NULL DEFAULT 3 CHECK (max_failures BETWEEN 1 AND 10),
    max_timeouts            INT  NOT NULL DEFAULT 3 CHECK (max_timeouts BETWEEN 1 AND 10),
    digit_len               INT  NOT NULL DEFAULT 1 CHECK (digit_len BETWEEN 1 AND 5),
    enabled                 BOOLEAN NOT NULL DEFAULT true,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, extension)
);
CREATE INDEX ivrs_tenant ON ivrs(tenant_id);

CREATE TABLE ivr_options (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ivr_id       UUID NOT NULL REFERENCES ivrs(id) ON DELETE CASCADE,
    digit        TEXT NOT NULL CHECK (digit ~ '^[0-9*#]+$'),
    label        TEXT,
    action_kind  TEXT NOT NULL
                   CHECK (action_kind IN
                     ('extension','ring_group','voicemail','ivr','hangup','dial_e164')),
    action_id    UUID,
    action_data  TEXT,                                -- e.g. E.164 for dial_e164
    UNIQUE (ivr_id, digit)
);
CREATE INDEX ivr_options_ivr ON ivr_options(ivr_id);

CREATE TRIGGER ivrs_set_updated_at
    BEFORE UPDATE ON ivrs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

INSERT INTO schema_meta(key, value) VALUES ('migration','0005_phase3_ivr')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
