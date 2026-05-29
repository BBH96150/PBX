-- Phase 2: PSTN — carrier accounts, DID linkage, seed CallCentric.

BEGIN;

-- Per-carrier accounts. CallCentric's model is "one DID = one account";
-- other carriers (Telnyx, Bandwidth) typically have one account holding many DIDs.
CREATE TABLE carrier_accounts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    carrier_id      UUID NOT NULL REFERENCES carriers(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,                       -- human label, e.g. "BBH main"
    sip_username    TEXT NOT NULL,
    sip_password    TEXT NOT NULL,
    auth_realm      TEXT,
    fs_gateway_name TEXT NOT NULL UNIQUE                 -- the name in sofia/gateway/<name>/
                      CHECK (fs_gateway_name ~ '^[a-z0-9_]+$'),
    register        BOOLEAN NOT NULL DEFAULT true,
    main_did_e164   TEXT CHECK (main_did_e164 IS NULL OR main_did_e164 ~ '^\+[1-9][0-9]{6,14}$'),
    enabled         BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (carrier_id, sip_username)
);
CREATE INDEX carrier_accounts_carrier ON carrier_accounts(carrier_id);

CREATE TRIGGER carrier_accounts_set_updated_at
    BEFORE UPDATE ON carrier_accounts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- DIDs now optionally reference a specific carrier_account (for outbound CID + inbound match).
ALTER TABLE dids
    ADD COLUMN carrier_account_id UUID REFERENCES carrier_accounts(id);
CREATE INDEX dids_carrier_account ON dids(carrier_account_id);

-- Seed CallCentric as a system carrier so phase 2 setup is "add account + DID".
INSERT INTO carriers (name, kind, sip_proxy_host, sip_proxy_port, transport, priority)
VALUES ('CallCentric', 'callcentric', 'callcentric.com', 5060, 'udp', 100)
ON CONFLICT (name) DO NOTHING;

INSERT INTO schema_meta(key, value) VALUES ('migration','0002_phase2_pstn')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
