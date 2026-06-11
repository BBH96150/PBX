-- Call disposition / wrap-up codes. A tenant defines a small set of outcome
-- labels (e.g. "Sale", "Support", "Voicemail", "Spam", "Follow-up"); an admin or
-- agent assigns ONE code to a completed call from the call-history (CDR) list.
-- This is post-call tagging only — there is NO change to live routing / the
-- dialplan. Mirrors the per-CDR `note` field added in 0034, but as an FK to a
-- tenant-defined code instead of free text.
BEGIN;

CREATE TABLE disposition_codes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    label       TEXT NOT NULL CHECK (label <> ''),
    color       TEXT,
    sort_order  INT  NOT NULL DEFAULT 0,
    active      BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, label)
);

CREATE INDEX idx_disposition_codes_tenant ON disposition_codes (tenant_id);

ALTER TABLE cdrs
    ADD COLUMN disposition_code_id UUID NULL
        REFERENCES disposition_codes(id) ON DELETE SET NULL;

COMMIT;
