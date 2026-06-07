-- PTT / paging groups: a named set of member extensions reachable as an
-- intercom/page. `mode` selects the talk-path delivery:
--   fs_conference — dial `extension`, members auto-answer into a one-way page conf
--   multicast     — desk phones listen on multicast_addr:multicast_port (ZTP)
--   native        — backed channel for the native softphone's hold-to-talk
BEGIN;

CREATE TABLE paging_groups (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    extension      TEXT CHECK (extension IS NULL OR extension ~ '^[0-9*#]+$'),
    name           TEXT NOT NULL,
    mode           TEXT NOT NULL DEFAULT 'fs_conference'
                     CHECK (mode IN ('fs_conference','multicast','native')),
    multicast_addr TEXT,
    multicast_port INT,
    enabled        BOOLEAN NOT NULL DEFAULT true,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, extension)
);

CREATE TABLE paging_group_members (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    paging_group_id UUID NOT NULL REFERENCES paging_groups(id) ON DELETE CASCADE,
    extension_id    UUID NOT NULL REFERENCES extensions(id) ON DELETE CASCADE,
    UNIQUE (paging_group_id, extension_id)
);
CREATE INDEX paging_group_members_ext ON paging_group_members(extension_id);

COMMIT;
