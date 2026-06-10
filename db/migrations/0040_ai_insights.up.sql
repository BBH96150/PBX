-- AI call summaries + voicemail transcription. Ships DISABLED: nothing writes
-- to these columns/tables unless the operator configures an AI transcription
-- provider + key (AI_TRANSCRIPTION_PROVIDER / DEEPGRAM_API_KEY / ANTHROPIC_API_KEY).
-- The background insights worker transcribes call recordings + voicemails and,
-- for calls, generates an AI summary + action items.
BEGIN;

CREATE TABLE call_insights (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID REFERENCES tenants(id) ON DELETE CASCADE,
    call_uuid    TEXT NOT NULL,
    transcript   TEXT,
    summary      TEXT,
    action_items JSONB NOT NULL DEFAULT '[]',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (call_uuid)
);

-- The worker resolves pending CDRs by tenant; the portal/API fetch by call_uuid.
CREATE INDEX idx_call_insights_tenant ON call_insights (tenant_id);

-- Per-message voicemail transcript. NULL until the worker transcribes it; the
-- existing voicemail scan paths don't read this column, so it stays inert.
ALTER TABLE voicemail_messages ADD COLUMN transcript TEXT NULL;

COMMIT;
