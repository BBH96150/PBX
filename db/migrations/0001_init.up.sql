-- Phase 1 initial schema.
-- Multi-tenant from day one. Kamailio-compatible views at the bottom.

BEGIN;

CREATE EXTENSION IF NOT EXISTS "pgcrypto";  -- gen_random_uuid, gen_random_bytes
CREATE EXTENSION IF NOT EXISTS "citext";    -- case-insensitive email

-- =========================================================================
-- Tenancy
-- =========================================================================

CREATE TABLE tenants (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        TEXT NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9-]+$'),
    name        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'active'
                  CHECK (status IN ('active','suspended','deleted')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sip_domains (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    domain      TEXT NOT NULL UNIQUE,
    is_primary  BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sip_domains_tenant ON sip_domains(tenant_id);

-- One primary domain per tenant.
CREATE UNIQUE INDEX sip_domains_one_primary_per_tenant
    ON sip_domains(tenant_id) WHERE is_primary;

-- =========================================================================
-- Users (humans with admin portal login)
-- =========================================================================

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email         CITEXT NOT NULL,
    display_name  TEXT NOT NULL,
    password_hash TEXT,                                   -- portal login (Phase 4)
    role          TEXT NOT NULL DEFAULT 'user'
                    CHECK (role IN ('user','tenant_admin','super_admin')),
    status        TEXT NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active','suspended','deleted')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, email)
);
CREATE INDEX users_tenant ON users(tenant_id);

-- =========================================================================
-- Extensions (SIP endpoints)
-- A user can own multiple extensions; extensions can also belong to no user
-- (e.g. conference-room phones, queue receivers, hot-desk pools).
-- =========================================================================

CREATE TABLE extensions (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    sip_domain_id      UUID NOT NULL REFERENCES sip_domains(id) ON DELETE RESTRICT,
    extension          TEXT NOT NULL CHECK (extension ~ '^[0-9*#]+$'),
    sip_username       TEXT NOT NULL,
    sip_password       TEXT NOT NULL,                    -- plaintext for ha1 regen + plain digest fallback
    ha1                TEXT NOT NULL,                    -- MD5(username:realm:password)
    ha1b               TEXT NOT NULL,                    -- MD5(username@realm:realm:password)
    user_id            UUID REFERENCES users(id) ON DELETE SET NULL,
    display_name       TEXT,
    voicemail_enabled  BOOLEAN NOT NULL DEFAULT false,
    voicemail_pin      TEXT,
    status             TEXT NOT NULL DEFAULT 'active'
                         CHECK (status IN ('active','suspended','deleted')),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, extension),
    UNIQUE (sip_domain_id, sip_username)
);
CREATE INDEX extensions_tenant ON extensions(tenant_id);
CREATE INDEX extensions_user ON extensions(user_id);

-- =========================================================================
-- Devices (physical phones; the entity ZTP provisions)
-- =========================================================================

CREATE TABLE devices (
    mac                  MACADDR PRIMARY KEY,
    tenant_id            UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    vendor               TEXT NOT NULL
                           CHECK (vendor IN ('polycom','grandstream','yealink','cisco','snom','fanvil','generic')),
    model                TEXT NOT NULL,
    firmware             TEXT,
    provisioning_token   TEXT NOT NULL DEFAULT encode(gen_random_bytes(24),'hex'),
    label                TEXT,
    last_provisioned_at  TIMESTAMPTZ,
    last_provisioned_ip  INET,
    user_agent           TEXT,
    notes                TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX devices_tenant ON devices(tenant_id);

CREATE TABLE device_lines (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_mac    MACADDR NOT NULL REFERENCES devices(mac) ON DELETE CASCADE,
    line_number   INT NOT NULL CHECK (line_number BETWEEN 1 AND 32),
    extension_id  UUID NOT NULL REFERENCES extensions(id) ON DELETE CASCADE,
    label         TEXT,
    UNIQUE (device_mac, line_number)
);
CREATE INDEX device_lines_extension ON device_lines(extension_id);

-- =========================================================================
-- Carriers / PSTN (Phase 2 logic; schema lands now)
-- =========================================================================

CREATE TABLE carriers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL UNIQUE,
    kind            TEXT NOT NULL
                      CHECK (kind IN ('callcentric','telnyx','bandwidth','twilio','generic_sip')),
    sip_proxy_host  TEXT NOT NULL,
    sip_proxy_port  INT  NOT NULL DEFAULT 5060,
    transport       TEXT NOT NULL DEFAULT 'udp'
                      CHECK (transport IN ('udp','tcp','tls')),
    auth_username   TEXT,
    auth_password   TEXT,
    auth_ip         INET,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    priority        INT NOT NULL DEFAULT 100,
    config          JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Inbound DIDs map to a tenant + a routing destination (polymorphic).
CREATE TABLE dids (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    carrier_id        UUID NOT NULL REFERENCES carriers(id) ON DELETE RESTRICT,
    e164              TEXT NOT NULL UNIQUE
                        CHECK (e164 ~ '^\+[1-9][0-9]{6,14}$'),
    destination_kind  TEXT NOT NULL
                        CHECK (destination_kind IN ('extension','ivr','queue','hunt_group','voicemail')),
    destination_id    UUID NOT NULL,
    cnam              TEXT,
    enabled           BOOLEAN NOT NULL DEFAULT true,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX dids_tenant ON dids(tenant_id);
CREATE INDEX dids_carrier ON dids(carrier_id);

CREATE TABLE outbound_routes (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    pattern             TEXT NOT NULL,            -- E.164 prefix; e.g. '+1', '+44'
    carrier_id          UUID NOT NULL REFERENCES carriers(id),
    priority            INT NOT NULL DEFAULT 100, -- lower = preferred
    caller_id_override  TEXT,
    enabled             BOOLEAN NOT NULL DEFAULT true,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX outbound_routes_tenant ON outbound_routes(tenant_id, priority);

-- =========================================================================
-- CDRs (call detail records; written by control plane from FreeSWITCH events)
-- =========================================================================

CREATE TABLE cdrs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID REFERENCES tenants(id) ON DELETE SET NULL,
    call_uuid       TEXT NOT NULL UNIQUE,
    direction       TEXT NOT NULL
                      CHECK (direction IN ('inbound','outbound','internal')),
    from_uri        TEXT NOT NULL,
    to_uri          TEXT NOT NULL,
    caller_id_num   TEXT,
    caller_id_name  TEXT,
    started_at      TIMESTAMPTZ NOT NULL,
    answered_at     TIMESTAMPTZ,
    ended_at        TIMESTAMPTZ,
    duration_sec    INT,
    billable_sec    INT,
    disposition     TEXT
                      CHECK (disposition IS NULL OR disposition IN
                        ('ANSWERED','NO_ANSWER','BUSY','FAILED','CANCELLED','CONGESTION')),
    hangup_cause    TEXT,
    carrier_id      UUID REFERENCES carriers(id),
    recording_path  TEXT,
    raw             JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX cdrs_tenant_started ON cdrs(tenant_id, started_at DESC);

-- =========================================================================
-- updated_at triggers
-- =========================================================================

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'tenants','users','extensions','devices','carriers','dids'
    ] LOOP
        EXECUTE format(
            'CREATE TRIGGER %I_set_updated_at BEFORE UPDATE ON %I
             FOR EACH ROW EXECUTE FUNCTION set_updated_at()',
            t, t
        );
    END LOOP;
END $$;

-- =========================================================================
-- Kamailio compatibility views
-- Kamailio's stock modules (auth_db, domain) read from these table names.
-- Our app tables stay the source of truth; these are read-only projections.
-- =========================================================================

-- auth_db expects: username, domain, password, ha1, ha1b
CREATE VIEW subscriber AS
SELECT
    e.id::text                   AS id,
    e.sip_username               AS username,
    d.domain                     AS domain,
    e.sip_password               AS password,
    ''::text                     AS email_address,
    e.ha1                        AS ha1,
    e.ha1b                       AS ha1b,
    NULL::text                   AS rpid
FROM extensions e
JOIN sip_domains d ON d.id = e.sip_domain_id
WHERE e.status = 'active';

-- domain module expects: id, domain, did, last_modified
CREATE VIEW domain AS
SELECT
    row_number() OVER (ORDER BY id)::int  AS id,
    domain                                AS domain,
    domain                                AS did,
    created_at                            AS last_modified
FROM sip_domains;

-- Kamailio's `version` table. Stock Kamailio modules (uri_db, domain,
-- usrloc, etc.) check `select table_version from version where table_name=…`
-- at startup and refuse to load if the row is missing. Our schema is a
-- superset of what those modules expect — we just need to advertise that
-- our subscriber + domain views match the versions they want.
CREATE TABLE version (
    table_name TEXT NOT NULL UNIQUE,
    table_version INTEGER NOT NULL DEFAULT 0
);
INSERT INTO version (table_name, table_version) VALUES
    ('subscriber', 7),
    ('domain', 2),
    ('uri', 1),
    ('location', 1008);

-- Version stamp so the control plane can verify schema state.
CREATE TABLE schema_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
INSERT INTO schema_meta(key, value) VALUES ('phase','1'), ('migration','0001_init');

COMMIT;
