# SIP / UCaaS Platform

A multi-tenant SIP server and cloud PBX, on a trajectory toward a full UCaaS
platform comparable to RingCentral / AT&T Office@Hand. Multi-tenant from day one
— every domain object and every SIP route is scoped by `tenant_id`.

It is built from five cooperating pieces: **Kamailio** (SIP edge / registrar),
**FreeSWITCH** (media + PBX features), a **Go control-plane** (REST API, web
portal, ZTP provisioning, and the dynamic brain behind FreeSWITCH), **PostgreSQL
+ Redis** (state), and **Caddy** (public TLS front door). It runs in Docker
Compose on a single Hetzner box, deployed automatically by GitHub Actions on
every push to `main`.

Production portal: `https://pbx.tendpos.com/admin/`
Tenant SIP domains: `<slug>.pbx.tendpos.com` (wildcard DNS → the box).

---

## Architecture at a glance

```
                         Internet
   phones / softphones      │       browser (admin + WebRTC softphone)
        (SIP/RTP)           │              (HTTPS / WSS)
            │               │                    │
            ▼               │                    ▼
   ┌──────────────┐         │            ┌──────────────┐
   │   Kamailio   │◄────────┼── /ws ─────│    Caddy     │  TLS (Let's Encrypt)
   │ SIP edge /   │         │            │ reverse proxy│
   │ registrar /  │         │            └──────┬───────┘
   │ WS:5066      │         │                   │ everything else
   └──────┬───────┘         │                   ▼
          │ dispatcher      │            ┌──────────────┐
          ▼                 │            │ control-plane│ :8080 (loopback)
   ┌──────────────┐         │            │  Go service  │ :8443 provisioning
   │  FreeSWITCH  │◄── xml_curl (dialplan/directory/config) ──┤
   │ media + PBX  │── ESL events ───────►│              │
   │ Sofia int/ext│         │            └──────┬───────┘
   └──────┬───────┘         │                   │
          │ Sofia external  │                   ▼
          ▼ + gateways      │            ┌──────────────┐
        PSTN carrier        │            │ Postgres +   │
        (CallCentric)       │            │   Redis      │
                            │            └──────────────┘
   ┌──────────────┐         │
   │  rtpengine   │◄── ng (Kamailio drives it for WS/WSS legs only)
   │ WebRTC media │
   └──────────────┘
```

- **Caddy** terminates TLS for `pbx.tendpos.com`, reverse-proxies the portal/API
  to `control-plane:8080`, and forwards `/ws*` to `kamailio:5066` so browser
  softphones can run SIP-over-WebSocket.
- **Kamailio** is the SIP registrar and edge router: it authenticates REGISTER
  and INVITE against the Postgres `subscriber` view, stores AOR bindings in
  `location`, and dispatches calls to the FreeSWITCH pool.
- **FreeSWITCH** does all media and PBX logic (RTP/SRTP, voicemail, IVR, ring
  groups, queues, conferences/paging, recording). It has **no static dialplan or
  directory** — it fetches them per-request from the control-plane over
  `mod_xml_curl`, and streams call events back over ESL.
- **control-plane** (Go) is the system's brain: it serves the `/v1` REST API, the
  server-rendered admin portal at `/admin/`, the FreeSWITCH `xml_curl`
  dialplan/directory/configuration endpoints, the ZTP device-provisioning HTTPS
  server, and a set of background goroutines (ESL listener, trunk monitor,
  webhook dispatcher, email digests).
- **rtpengine** bridges browser WebRTC media (DTLS-SRTP / ICE / rtcp-mux) to
  plain RTP toward FreeSWITCH. Kamailio invokes it only for WS/WSS legs; desk
  phones never touch it.

---

## Component map

| Component | Tech | Lives in | README |
| --- | --- | --- | --- |
| SIP edge / registrar | Kamailio | `kamailio/` | [kamailio/README.md](kamailio/README.md) |
| Media + PBX | FreeSWITCH | `freeswitch/` | [freeswitch/README.md](freeswitch/README.md) |
| Control plane (API, portal, ZTP, FS brain) | Go | `control-plane/` | [control-plane/README.md](control-plane/README.md) |
| Schema migrations | golang-migrate SQL | `db/` | [db/README.md](db/README.md) |
| Public TLS reverse proxy | Caddy | `caddy/` | [caddy/README.md](caddy/README.md) |
| WebRTC media bridge | rtpengine | (compose only) | — |
| State store | PostgreSQL 16 + Redis 7 | (compose only) | — |
| Test OIDC IdP (dev) | Dex | `dex/` | — |

---

## Repository layout

```
.
├── docker-compose.yml                  # Local dev stack (postgres, redis, kamailio,
│                                       #   freeswitch, control-plane, caddy, +dex/migrate)
├── deploy/
│   ├── docker-compose.prod-base.yml    # Source of truth for the box's base compose
│   └── prod-compose.snapshot.yml       # Reference snapshot of the running box config
├── docker-compose.prod.yml             # ADDITIVE prod override (rtpengine, extra mounts)
├── .env.example                        # Copy to .env and fill in
├── kamailio/etc/                       # kamailio.cfg + dispatcher.list
├── freeswitch/conf/                    # FreeSWITCH XML config tree (Sofia profiles, modules,
│                                       #   conference/voicemail/xml_curl, dialplan stubs)
├── control-plane/                      # Go service (cmd/server + internal/*)
├── db/migrations/                      # Numbered up/down SQL migrations
├── caddy/Caddyfile                     # TLS reverse proxy
├── dex/                                # Dex config for SSO smoke tests
├── docs/                               # API.md, openapi.yaml, onboarding.md, kb/
├── secrets/                            # SAML SP keypair (gitignored)
└── .github/workflows/                  # CI + ops/runbook workflows
```

---

## Quick start (local dev)

Prerequisites: Docker + Docker Compose. On macOS, [OrbStack](https://orbstack.dev)
gives far better SIP/RTP behavior than Docker Desktop, which cannot give
containers real host networking (RTP audio will not flow cleanly).

```bash
cp .env.example .env            # then fill in secrets / bootstrap creds
docker compose up -d postgres redis
docker compose --profile tools run --rm migrate up   # apply schema
docker compose up -d control-plane kamailio freeswitch caddy
```

- Admin portal / API: `http://localhost:8080/admin/` and `http://localhost:8080/healthz`
- Provisioning (TLS in prod, plain HTTP in dev): `:8443`
- SSO smoke tests: `docker compose --profile sso-test up -d dex`

> **NOTE — Postgres in some dev envs:** an ephemeral local Postgres can fail to
> start in restricted environments (kernel `SHMALL`). When you can't run PG
> locally, validate schema/SQL changes through CI (the `migrations` job spins up
> a real Postgres and runs `migrate up`) rather than locally.

Each component's README has its own build/test/run details. For the Go service:
`cd control-plane && go build ./... && go vet ./... && go test ./...`.

---

## CI / deploy

GitHub Actions (`.github/workflows/ci.yml`) runs on every push and PR. On a push
to `main` it builds and ships:

1. **lint** — `go vet` + golangci-lint.
2. **test** — `go build` + `go test -race`.
3. **migrations** — spins up Postgres 16 and runs `migrate up` against
   `db/migrations/` to prove the schema applies cleanly.
4. **vuln** — `govulncheck`.
5. **docker** — multi-arch (amd64/arm64) build of `control-plane`, pushed to
   GHCR (`ghcr.io/bbh96150/pbx/control-plane`), pinned by immutable digest.
6. **deploy** (main only) — over SSH to the Hetzner box:
   reclaim disk → rsync `db/migrations/` → rsync `docker-compose.prod.yml` as
   the additive override → rsync `deploy/docker-compose.prod-base.yml` as the
   base compose → `docker pull <digest>` and retag `:latest` → run `migrate up`
   → `docker compose up -d --force-recreate control-plane` → `/healthz` smoke
   check.

**Deploy gotchas baked into the pipeline** (don't undo them):
- The image is pulled by **digest**, not `:latest`, so concurrent builds can't
  race a stale image onto the box.
- `migrate` runs with `-T </dev/null` so it doesn't slurp the rest of the SSH
  heredoc and silently eat the restart step.
- `--force-recreate` is required: `up -d` after a retag of an unchanged `:latest`
  string does not reliably recreate the container.
- Only `control-plane` is image-deployed by CI. **Kamailio and FreeSWITCH config
  is NOT synced by `ci.yml`** — change those via the ops/restart workflows.

---

## Ops workflows (manual `workflow_dispatch` runbooks)

These are guarded, mostly-read-only operational runbooks under
`.github/workflows/`. Most require typing `yes` into an input before they make
any change.

| Workflow | File | What it does |
| --- | --- | --- |
| Ops · disk | `ops-disk.yml` | Diagnose disk usage; optionally run safe cleanups (journal/container logs/apt), install a global Docker log-size cap (50m ×3), and force-recreate containers to adopt it. |
| Ops · kamailio usrloc | `ops-kamailio.yml` | Read-only dump of the box's usrloc config + `location` table; optionally flip `WITH_USRLOCDB` 0→1 and restart (auto-rolls-back if unhealthy), or restore the pre-flip backup. |
| Ops · fs diag | `ops-fs-diag.yml` | Read-only dump of `mod_callcenter` list formats + a channels sample for queue debugging. |
| Ops · env | `ops-env.yml` | Report which `SMTP_*` / `ALERT_EMAIL` keys are set in the box `.env` (status only, no values); optionally set `ALERT_EMAIL` and recreate control-plane. |
| Ops · paging validate | `ops-paging.yml` | Install `conference.conf` + load `mod_conference`; read-only discovery; reload to prove static-file fallback for paging groups. |
| Ops · webrtc media | `ops-webrtc.yml` | Pull/inspect the rtpengine image, bring it up, and show logs + `ng` reachability for WebRTC media. |
| Monitor · health | `monitor.yml` | Scheduled health probe. |
| Restart SIP services | `restart-sip.yml` | Restart selected services; optionally sync conf dirs from the checkout first (destructive — overwrites server-side edits). |
| Rollback | `rollback.yml` | Roll the control-plane image back to a prior tag (default: previous `main` build). |

---

## Docs

- [`docs/API.md`](docs/API.md) — REST `/v1` API + outbound webhook reference (auth, endpoints, event payloads, HMAC signature verification).
- [`docs/openapi.yaml`](docs/openapi.yaml) — machine-readable OpenAPI spec.
- [`docs/onboarding.md`](docs/onboarding.md) — per-customer onboarding runbook (tenant → trunk → DIDs → extensions → device → end-to-end test) with the CallCentric landmines.
- [`docs/kb/`](docs/kb/) — knowledge-base articles.

---

## Production ops facts

- Single Hetzner box (`5.78.207.2`); Compose project `sip-platform`.
- DNS: `*.pbx.tendpos.com` and `pbx.tendpos.com` → the box; Caddy auto-issues
  Let's Encrypt certs.
- First PSTN carrier: **CallCentric** (`199.87.128.0/19` in the `trusted-pstn`
  ACL). The control-plane writes per-tenant FreeSWITCH gateway XML dynamically;
  the carrier layer is abstracted so Telnyx/Bandwidth can be added later.
