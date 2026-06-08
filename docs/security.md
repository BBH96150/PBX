# Security model

How the platform handles authentication, authorization, secrets, and the
common abuse vectors. This is a map of the controls that exist in the code, not
a compliance statement.

## Authentication

- **API bearer tokens** — `sip_<hex>` tokens for `/v1`. Stored hashed; the
  plaintext is shown once at creation. Verified per request.
- **Portal session** — an `httpOnly`, `Secure`-when-HTTPS cookie carrying a
  token whose name encodes the user (`portal:<email>:<ts>`); revoked on logout.
- **Password login** — passwords are bcrypt-hashed; SIP digest auth uses
  per-user HA1 (`MD5(user:realm:password)`), so plaintext SIP passwords aren't
  needed at auth time.
- **Two-factor (TOTP)** — optional per user, enforceable per tenant. Secrets are
  sealed with **AES-256-GCM** (`internal/crypto`) before they touch the DB, so a
  leaked DB dump alone doesn't yield working codes — the `TOTP_ENCRYPTION_KEY`
  lives only in the process env.
- **SSO** — OIDC (with PKCE/state/nonce) and SAML 2.0, with JIT provisioning.
- **Login hardening** — per-email + per-IP login rate limiting; session
  listing/revocation; email-verification gate.

## Authorization

- **Scopes** — every token has `read` < `write` < `admin` (hierarchical;
  `RequireScope` checks rank). Role→scope: super/tenant admin → `admin`, user →
  `write`, else `read`.
- **Tenant isolation** — every domain object is `tenant_id`-scoped. A
  tenant-scoped token can only address its own tenant (`403` otherwise); store
  methods are `…ForTenant` and enforce ownership (cross-tenant operations return
  `ErrCrossTenant`).
- **Self-service confinement** — non-admin portal users are pinned to the
  `/me`, `/security`, `/softphone`, `/broadcast`, `/help` self-service area;
  ownership of an extension is re-verified server-side on every access.

## Outbound webhooks

- **Signed** — each delivery carries `X-Webhook-Signature: sha256=<hmac>` over
  the raw body, keyed by the endpoint's secret.
- **SSRF-guarded** — only public HTTPS URLs are dialed; loopback, private, and
  link-local addresses are refused at the dialer; redirects are not followed.
- Delivery is best-effort with a short timeout and bounded retries; the last
  status is recorded.

## Input & output safety

- **CSV injection** — exported cells are prefixed to neutralize `=`/`+`/`-`/`@`
  formula payloads (`csvSafe`).
- **Path traversal** — Help Center slugs and gateway filenames are validated
  against a strict character set; voicemail/recording paths must resolve under a
  configured root before streaming.
- **Sensitive-field stripping** — device provisioning tokens are omitted from
  bulk API listings; voicemail audio paths are never serialized; SIP passwords
  are returned only at create time.
- **Markdown** — the in-app Help Center renders KB markdown with raw HTML
  disabled.

## Network & transport

- **TLS everywhere public** — Caddy terminates HTTPS (Let's Encrypt) for the
  portal/API and the SIP-over-WebSocket path (`/ws`).
- **WebRTC media** — DTLS-SRTP terminated at rtpengine; only WS/WSS legs are
  bridged.
- **SIP edge** — Kamailio authenticates REGISTER/INVITE against the subscriber
  table and runs `pike` anti-flood; the control-plane API binds loopback behind
  Caddy.

## Secrets handling

- Real secrets (DB password, SMTP creds, SAML keys, signing keys) live only on
  the box in a gitignored `.env` / `secrets/`. The repo and committed compose
  files carry only `${VAR}` placeholders.
- The box's Kamailio config holds the real DB password, injected from the
  container env at deploy time — the repo copy is a placeholder, and ops
  tooling re-injects on every sync (never overwrite it blindly).
- Production data (PII: CDRs, recordings, voicemail) is not pulled off the box.

## Reporting

For a real deployment, route security reports to the operator contact in your
runbook. This document tracks the in-code controls; pair it with infrastructure
controls (firewall, fail2ban, backups) at the host level.
