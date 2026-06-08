# control-plane

The Go service that is the brain of the platform. It is a single binary
(`cmd/server`) that runs several cooperating servers and background workers in
one process:

- **`/v1` REST API** — tenant/extension/DID/device/contact/ring-group/queue/IVR
  CRUD, CDR pull, voicemail metadata, API-token and webhook management
  (see [`docs/API.md`](../docs/API.md) and [`docs/openapi.yaml`](../docs/openapi.yaml)).
- **Admin portal** at `/admin/*` — server-rendered HTML for tenants, extensions,
  trunks, DIDs, ring groups, queues, IVRs, paging groups, the WebRTC softphone,
  voicemail inbox, call recordings, reports, audit log, webhooks, 2FA, SSO
  (OIDC + SAML), invitations, sessions, and self-service onboarding.
- **FreeSWITCH `xml_curl` endpoints** — `/v1/freeswitch/dialplan`,
  `/v1/freeswitch/directory`, `/v1/freeswitch/configuration`. FreeSWITCH has no
  static dialplan/directory; it fetches per-call XML from here.
- **ESL listener** — connects to FreeSWITCH event socket, records CDRs, fires
  webhooks, sends voicemail-to-email, and powers the live-calls / queue views.
- **ZTP provisioning HTTPS server** on `:8443` — serves per-MAC device config to
  Polycom/Yealink/Grandstream phones; optional vendor redirection (RPS/GDMS/ZTP).
- **Background workers** — per-tenant carrier gateway provisioner (writes Sofia
  gateway XML + triggers rescan over ESL), trunk-up/down monitor, webhook
  dispatcher, and a scheduled email digest sender.

## Where it sits

Caddy reverse-proxies the public portal/API to this service on `:8080` (bound to
loopback on the box). FreeSWITCH calls it over `mod_xml_curl` for dialplan and
streams events back over ESL. It reads/writes Postgres (everything durable) and
Redis (registrations / call state / presence / rate limits).

## Key packages

| Path | Responsibility |
| --- | --- |
| `cmd/server/main.go` | Wiring: opens store, bootstraps admin token/user, builds the admin mux + provisioning server, launches the ESL/trunk-monitor/digest goroutines, graceful shutdown. |
| `cmd/gen-saml-keys/` | Helper to generate the SAML SP keypair. |
| `internal/api/` | `/v1` REST handlers (`server.go` is the route table). |
| `internal/portal/` | `/admin/*` server-rendered portal (one file per feature area + `templates/`, `static/`). |
| `internal/store/` | Postgres + Redis data access (one file per aggregate). |
| `internal/freeswitch/` | `xml_curl` dialplan/directory/configuration handlers, ESL client, CDR ingest, gateway provisioner, trunk monitor, ring-group/queue/voicemail logic, SMS. |
| `internal/provisioning/` | ZTP HTTPS server + per-vendor config templates. |
| `internal/rps/` | Vendor redirection adapters (Polycom ZTP, Yealink RPS, Grandstream GDMS) with a log-only fallback. |
| `internal/webhook/` | Outbound webhook dispatcher (HMAC-SHA256 signing, SSRF guard). |
| `internal/digest/` | Scheduled email digest sender. |
| `internal/sso/` | OIDC + SAML 2.0 SSO. |
| `internal/digest`, `internal/audit/`, `internal/smtp/` | Email digests, audit log, SMTP mailer. |
| `internal/crypto/` | AES-GCM sealer (used for TOTP secrets at rest). |
| `internal/config/` | Env-var configuration loader. |
| `internal/e164/` | E.164 phone-number normalization. |

## Build / test

```bash
cd control-plane
go build ./...
go vet ./...
go test ./...          # CI runs `go test -race -count=1 ./...`
```

The module is `github.com/tendpos/sip-platform/control-plane` (Go 1.25; build
toolchain pinned to 1.25.11). CI additionally runs golangci-lint and
govulncheck.

> **NOTE — Postgres in some dev envs:** an ephemeral local Postgres can fail to
> boot in restricted environments (kernel `SHMALL`). Unit tests that need a DB,
> and any migration/SQL change, are best validated through CI (the `migrations`
> job runs real Postgres) rather than locally.

## Run locally

Easiest via the root compose (`docker compose up -d control-plane` after
`postgres`/`redis` are healthy). The image is built from `control-plane/Dockerfile`
(multi-stage, Alpine). To run the binary directly, set the env vars below and
point `DATABASE_URL`/`REDIS_URL` at running instances.

## Configuration (env vars)

Read by `internal/config/config.go`:

| Var | Purpose |
| --- | --- |
| `DATABASE_URL` | Postgres DSN. |
| `REDIS_URL` | Redis DSN. |
| `CONTROL_PLANE_ADMIN_ADDR` | Admin/API/portal listen addr (default `:8080`). |
| `CONTROL_PLANE_PROVISIONING_ADDR` | Provisioning HTTPS listen addr (default `:8443`). |
| `CONTROL_PLANE_LOG_LEVEL` | Log level. |
| `PROVISIONING_TLS_CERT` / `PROVISIONING_TLS_KEY` | Provisioning TLS; if unset, serves plain HTTP (dev only). |
| `PROVISIONING_PUBLIC_HOST` | Public host used in generated provisioning URLs. |
| `ESL_HOST` / `ESL_PORT` / `ESL_PASSWORD` | FreeSWITCH event socket. |
| `KAMAILIO_SIP_TARGET` | Kamailio SIP target used in dialplan routing (e.g. `kamailio:5060`). |
| `SIP_PUBLIC_HOST` / `SIP_PUBLIC_PORT` / `SIP_PUBLIC_TRANSPORT` | Public SIP coordinates advertised to phones/provisioning. |
| `SIP_DOMAIN_SUFFIX` | Wildcard suffix for auto-generated tenant SIP domains (e.g. `pbx.tendpos.com`). |
| `PORTAL_BASE_URL` | Base URL used in portal-generated links / emails. |
| `BOOTSTRAP_API_TOKEN` | Seeds the first admin API token if the table is empty. |
| `BOOTSTRAP_USER_EMAIL` / `BOOTSTRAP_USER_PASSWORD` | Seeds the first super-admin portal user if the users table is empty. |
| `SMTP_HOST` / `SMTP_PORT` / `SMTP_USERNAME` / `SMTP_PASSWORD` / `SMTP_FROM` / `SMTP_STARTTLS` | Outbound email (invites, verify-email, voicemail-to-email, digests). |
| `ALERT_EMAIL` | Destination for trunk-down alerts. |
| `TOTP_ENCRYPTION_KEY` | AES-GCM key sealing TOTP secrets. **If users have enrolled 2FA and this is unset, the service refuses to start.** |
| `SAML_SP_CERT_PEM` / `SAML_SP_KEY_PEM` (or `SAML_SP_CERT_FILE` / `SAML_SP_KEY_FILE`) | SAML SP keypair; unset ⇒ SAML SSO disabled. |
| `FS_DYNAMIC_GATEWAY_DIR` | Shared dir where per-tenant carrier gateway XML is written for FreeSWITCH. |
| `FS_LOG_DIR` | Read-only FS log dir, mined for trunk registration-failure reasons in the UI. |
| `VOICEMAIL_STORAGE_ROOT` / `RECORDING_STORAGE_ROOT` | Shared FS storage roots for VM inbox playback and CDR recording playback / paging blasts. |
| `POLYCOM_ZTP_*`, `YEALINK_RPS_*`, `GRANDSTREAM_GDMS_*` | Optional vendor redirection credentials; absent ⇒ that adapter falls back to log-only. |

## Deploy

CI builds a multi-arch image and pushes it to GHCR by digest, then SSHes to the
box to `docker pull` the digest, run `migrate up`, and
`docker compose up -d --force-recreate control-plane`. This is the **only**
image CI deploys. See the root [README](../README.md#ci--deploy).

## Gotchas

- **Startup guards:** the service refuses to start if 2FA is enrolled without
  `TOTP_ENCRYPTION_KEY`. SAML/OIDC routes refuse if their keys/issuers are unset
  (logged as warnings, not fatal).
- **`xml_curl` timeouts are tight** (FreeSWITCH side: ~2s). Keep dialplan/
  directory/configuration handlers fast — a slow DB query stalls live calls.
- **Bootstrap is idempotent-on-empty:** bootstrap token/user only seed when their
  tables are empty; rotating creds means doing it through the portal, not env.
