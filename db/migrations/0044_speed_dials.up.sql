-- Personal speed dials / favorites. Each logged-in end-user maintains a small
-- private list of speed-dial entries (label + number/extension) shown on their
-- self-service page with a one-click CALL button (reusing click-to-dial). This
-- is per-USER, not per-tenant and not admin-managed: every row is owned by a
-- single user. tenant_id is carried alongside user_id for integrity / RLS-style
-- scoping, but all reads/writes are keyed on user_id. No dialplan involvement.
BEGIN;

CREATE TABLE speed_dials (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    label       TEXT NOT NULL CHECK (label <> ''),
    number      TEXT NOT NULL CHECK (number <> ''),
    sort_order  INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_speed_dials_user ON speed_dials (user_id, sort_order);

COMMIT;
