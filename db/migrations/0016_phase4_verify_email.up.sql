-- Phase 4.9: explicit verify-email-on-signup with one-time tokens.
--
--   email_verification_tokens : 24h-lived, bcrypt-hashed, prefix-narrowed
--                                (same pattern as user_invites + password_reset_tokens)
--
-- Resend policy: old tokens stay valid until they expire — rationale captured
-- in the user-decided wave plan. No invalidate-on-resend trigger here.

BEGIN;

CREATE TABLE email_verification_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_prefix TEXT NOT NULL,
    token_hash   TEXT NOT NULL,             -- bcrypt of full plaintext
    expires_at   TIMESTAMPTZ NOT NULL DEFAULT now() + INTERVAL '24 hours',
    consumed_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX email_verification_tokens_prefix ON email_verification_tokens(token_prefix);
CREATE INDEX email_verification_tokens_user_pending
    ON email_verification_tokens(user_id, created_at DESC)
    WHERE consumed_at IS NULL;

INSERT INTO schema_meta(key, value) VALUES ('migration','0016_phase4_verify_email')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
