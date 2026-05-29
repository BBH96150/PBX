BEGIN;
ALTER TABLE carrier_accounts
    ADD COLUMN transport_override  TEXT
        CHECK (transport_override IS NULL OR transport_override IN ('udp','tcp','tls')),
    ADD COLUMN proxy_port_override INTEGER
        CHECK (proxy_port_override IS NULL OR (proxy_port_override > 0 AND proxy_port_override < 65536));
INSERT INTO schema_meta(key, value) VALUES ('migration','0021_phase5_carrier_transport_port')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
COMMIT;
