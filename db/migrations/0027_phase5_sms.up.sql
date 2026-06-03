-- Phase 5: SMS foundation.
--
-- Stores inbound + outbound text messages. A "conversation" is the set of
-- messages between one of our DIDs (our_e164) and an external peer (peer_e164)
-- for a tenant — derived by grouping, no separate table.
--
-- The carrier/FreeSWITCH transport (SIP MESSAGE / SIP-SIMPLE, mod_sms chatplan)
-- is NOT wired yet; this is the control-plane side. Messages are inert until a
-- number is SMS-enabled and FS routes inbound to /v1/sms/inbound.

BEGIN;

CREATE TABLE sms_messages (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    our_e164     TEXT NOT NULL,                 -- the tenant DID involved
    peer_e164    TEXT NOT NULL,                 -- the external party
    direction    TEXT NOT NULL CHECK (direction IN ('inbound','outbound')),
    body         TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'received'
                   CHECK (status IN ('received','queued','sent','delivered','failed')),
    provider_id  TEXT,                          -- carrier message id, when known
    error        TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sms_messages_thread
    ON sms_messages(tenant_id, our_e164, peer_e164, created_at);
CREATE INDEX sms_messages_tenant_recent
    ON sms_messages(tenant_id, created_at DESC);

INSERT INTO schema_meta(key, value) VALUES ('migration','0027_phase5_sms')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
