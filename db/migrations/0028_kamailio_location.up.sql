-- Kamailio usrloc DB persistence (for live extension presence).
--
-- Until now Kamailio ran usrloc in memory only (WITH_USRLOCDB 0), so the set of
-- registered extensions lived only in Kamailio's RAM. Creating the standard
-- `location` table lets us flip usrloc to db_mode=1 (write-through) so the
-- control-plane can read "who's online" with a plain SQL query.
--
-- Schema is the verbatim Kamailio 5.5.2 location table (utils/kamctl/postgres/
-- usrloc-create.sql @ tag 5.5.2). Must match exactly — usrloc checks the table
-- version on load and refuses to start in db_mode if it disagrees.

BEGIN;

CREATE TABLE location (
    id SERIAL PRIMARY KEY NOT NULL,
    ruid VARCHAR(64) DEFAULT '' NOT NULL,
    username VARCHAR(64) DEFAULT '' NOT NULL,
    domain VARCHAR(64) DEFAULT NULL,
    contact VARCHAR(512) DEFAULT '' NOT NULL,
    received VARCHAR(128) DEFAULT NULL,
    path VARCHAR(512) DEFAULT NULL,
    expires TIMESTAMP WITHOUT TIME ZONE DEFAULT '2030-05-28 21:32:15' NOT NULL,
    q REAL DEFAULT 1.0 NOT NULL,
    callid VARCHAR(255) DEFAULT 'Default-Call-ID' NOT NULL,
    cseq INTEGER DEFAULT 1 NOT NULL,
    last_modified TIMESTAMP WITHOUT TIME ZONE DEFAULT '2000-01-01 00:00:01' NOT NULL,
    flags INTEGER DEFAULT 0 NOT NULL,
    cflags INTEGER DEFAULT 0 NOT NULL,
    user_agent VARCHAR(255) DEFAULT '' NOT NULL,
    socket VARCHAR(64) DEFAULT NULL,
    methods INTEGER DEFAULT NULL,
    instance VARCHAR(255) DEFAULT NULL,
    reg_id INTEGER DEFAULT 0 NOT NULL,
    server_id INTEGER DEFAULT 0 NOT NULL,
    connection_id INTEGER DEFAULT 0 NOT NULL,
    keepalive INTEGER DEFAULT 0 NOT NULL,
    partition INTEGER DEFAULT 0 NOT NULL,
    CONSTRAINT location_ruid_idx UNIQUE (ruid)
);

CREATE INDEX location_account_contact_idx ON location (username, domain, contact);
CREATE INDEX location_expires_idx ON location (expires);
CREATE INDEX location_connection_idx ON location (server_id, connection_id);

-- The initial schema advertised location v1008 (incorrect — that value predates
-- this work and would make usrloc reject the table in db_mode). The real
-- table_version for Kamailio 5.5.x usrloc is 9.
UPDATE version SET table_version = 9 WHERE table_name = 'location';

COMMIT;
