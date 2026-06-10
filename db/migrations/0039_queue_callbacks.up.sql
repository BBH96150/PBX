-- Queue callbacks ("keep your place in line"). Instead of holding in a queue, a
-- caller can request a callback: the system records the request, then later
-- dials the caller back and transfers them into the queue to reach the next
-- agent. The dialplan's callback-request route writes a 'pending' row; a portal
-- "Call now" button (and an optional background worker) originate the callback
-- via the tenant's carrier gateway and &transfer the answered leg into the queue.
BEGIN;

CREATE TABLE queue_callbacks (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    queue_id        UUID NOT NULL REFERENCES queues(id) ON DELETE CASCADE,
    caller_number   TEXT NOT NULL,
    caller_name     TEXT,
    status          TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending','dialing','connected','failed','cancelled')),
    attempts        INT NOT NULL DEFAULT 0,
    requested_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_attempt_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The worker / list pages query by (tenant, status) — e.g. all 'pending' rows.
CREATE INDEX idx_queue_callbacks_tenant_status ON queue_callbacks (tenant_id, status);

COMMIT;
