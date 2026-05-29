-- Phase 3 Wave 4: call queues (ACD via mod_callcenter).
--
-- queues + queue_agents mirror the structure mod_callcenter expects. We
-- serve <queue>/<agent>/<tier> blocks from callcenter.conf via mod_xml_curl;
-- admins run `reload mod_callcenter` after changes (live ESL sync lands in
-- Wave 4.5).
--
-- Agent + queue names in mod_callcenter must be globally unique strings.
-- We use queue.id and a synthetic "agent_<extension_id>" so they're
-- collision-free across tenants without exposing UUIDs to the dial plan.

BEGIN;

CREATE TABLE queues (
    id                              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    extension                       TEXT CHECK (extension IS NULL OR extension ~ '^[0-9*#]+$'),
    name                            TEXT NOT NULL,
    strategy                        TEXT NOT NULL DEFAULT 'longest-idle-agent'
                                      CHECK (strategy IN (
                                        'ring-all','longest-idle-agent',
                                        'agent-with-least-talk-time',
                                        'agent-with-fewest-calls',
                                        'sequentially-by-agent-order',
                                        'random','round-robin','top-down')),
    moh_sound                       TEXT NOT NULL DEFAULT 'local_stream://moh',
    record_template                 TEXT,
    time_base_score                 TEXT NOT NULL DEFAULT 'queue'
                                      CHECK (time_base_score IN ('queue','system')),
    max_wait_time                   INT  NOT NULL DEFAULT 0,            -- 0 = no max
    max_wait_no_agent               INT  NOT NULL DEFAULT 0,
    max_wait_no_agent_time_reached  INT  NOT NULL DEFAULT 5,
    tier_rules_apply                BOOLEAN NOT NULL DEFAULT false,
    tier_rule_wait_second           INT  NOT NULL DEFAULT 0,
    tier_rule_no_agent_no_wait      BOOLEAN NOT NULL DEFAULT true,
    discard_abandoned_after         INT  NOT NULL DEFAULT 14400,        -- 4h
    abandoned_resume_allowed        BOOLEAN NOT NULL DEFAULT false,
    announce_sound                  TEXT,                                -- played before MoH (e.g. "thanks for calling")
    enabled                         BOOLEAN NOT NULL DEFAULT true,
    created_at                      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, extension)
);
CREATE INDEX queues_tenant ON queues(tenant_id);

CREATE TABLE queue_agents (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    queue_id              UUID NOT NULL REFERENCES queues(id) ON DELETE CASCADE,
    extension_id          UUID NOT NULL REFERENCES extensions(id) ON DELETE CASCADE,
    agent_type            TEXT NOT NULL DEFAULT 'callback'
                            CHECK (agent_type IN ('callback','uuid-standby')),
    tier_level            INT  NOT NULL DEFAULT 1,    -- lower = higher priority tier
    tier_position         INT  NOT NULL DEFAULT 1,    -- within a tier
    max_no_answer         INT  NOT NULL DEFAULT 3,
    wrap_up_time          INT  NOT NULL DEFAULT 10,   -- seconds after each call
    reject_delay_time     INT  NOT NULL DEFAULT 10,
    busy_delay_time       INT  NOT NULL DEFAULT 60,
    no_answer_delay_time  INT  NOT NULL DEFAULT 30,
    enabled               BOOLEAN NOT NULL DEFAULT true,
    UNIQUE (queue_id, extension_id)
);
CREATE INDEX queue_agents_extension ON queue_agents(extension_id);

CREATE TRIGGER queues_set_updated_at
    BEFORE UPDATE ON queues
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Allow ivr_options to target a queue (the enum from 0005 didn't include it
-- because queues didn't exist yet).
ALTER TABLE ivr_options DROP CONSTRAINT IF EXISTS ivr_options_action_kind_check;
ALTER TABLE ivr_options ADD CONSTRAINT ivr_options_action_kind_check
    CHECK (action_kind IN ('extension','ring_group','voicemail','ivr','hangup','dial_e164','queue'));

INSERT INTO schema_meta(key, value) VALUES ('migration','0006_phase3_queues')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
