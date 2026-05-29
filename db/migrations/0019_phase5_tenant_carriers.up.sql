-- Phase 5.1: tenant-scope the existing carrier_accounts table so each
-- tenant can self-serve their own SIP trunk (CallCentric, Telnyx, etc.).
--
-- Backwards compat: nullable tenant_id; existing platform-wide rows stay
-- as-is (tenant_id IS NULL = legacy/platform). New rows must set tenant_id.

BEGIN;

ALTER TABLE carrier_accounts
    ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;

CREATE INDEX carrier_accounts_tenant ON carrier_accounts(tenant_id);

INSERT INTO schema_meta(key, value) VALUES ('migration','0019_phase5_tenant_carriers')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
