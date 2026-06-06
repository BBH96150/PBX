-- Outbound webhooks: tenant-configured HTTP endpoints that receive signed
-- JSON event callbacks (call.completed, trunk.down/up, voicemail.new, ...).
BEGIN;

CREATE TABLE webhook_endpoints (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    url        TEXT NOT NULL CHECK (url ~ '^https://'),
    secret     TEXT NOT NULL,                       -- HMAC-SHA256 signing key
    events     TEXT[] NOT NULL DEFAULT '{}',        -- subscribed event types; empty = all
    enabled    BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX webhook_endpoints_tenant ON webhook_endpoints(tenant_id);

COMMIT;
