-- Phase 4.7: TOTP 2FA + recovery codes + trusted devices.
--
--   user_2fa_methods       : one row per enrolled factor; schema is extensible
--                            beyond TOTP (kind enum covers webauthn/sms later)
--   user_2fa_recovery_codes: 10 single-use bcrypt-hashed codes per enrollment
--   user_trusted_devices   : "remember this device" cookies; 30-day expiry
--   tenants.require_2fa    : per-tenant soft-enforcement (grace mode)
--
-- Secret storage:
--   secret_ciphertext + secret_nonce are AES-GCM(env-key). A leaked DB
--   dump alone does not yield working codes — the app needs
--   TOTP_ENCRYPTION_KEY at runtime to seal/open. Process refuses to start
--   if any rows exist and the key isn't set.

BEGIN;

CREATE TABLE user_2fa_methods (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind               TEXT NOT NULL DEFAULT 'totp'
                         CHECK (kind IN ('totp','webauthn','sms')),
    secret_ciphertext  BYTEA NOT NULL,
    secret_nonce       BYTEA NOT NULL,
    label              TEXT NOT NULL DEFAULT '',
    confirmed_at       TIMESTAMPTZ,
    last_used_at       TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX user_2fa_methods_user ON user_2fa_methods(user_id);
CREATE UNIQUE INDEX user_2fa_methods_kind_label
    ON user_2fa_methods(user_id, kind, label);

CREATE TABLE user_2fa_recovery_codes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash   TEXT NOT NULL,  -- bcrypt
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX user_2fa_recovery_codes_user ON user_2fa_recovery_codes(user_id);

CREATE TABLE user_trusted_devices (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_prefix TEXT NOT NULL,        -- first 8 hex of plaintext for fast lookup
    token_hash   TEXT NOT NULL,        -- bcrypt of full plaintext
    label        TEXT NOT NULL DEFAULT '',
    ip_address   INET,
    expires_at   TIMESTAMPTZ NOT NULL DEFAULT now() + INTERVAL '30 days',
    last_seen_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX user_trusted_devices_user   ON user_trusted_devices(user_id);
CREATE INDEX user_trusted_devices_prefix ON user_trusted_devices(token_prefix);

ALTER TABLE tenants
    ADD COLUMN require_2fa BOOLEAN NOT NULL DEFAULT false;

INSERT INTO schema_meta(key, value) VALUES ('migration','0014_phase4_twofa')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
