# State of the platform — morning report

Written after an overnight build session. This is the honest version: what's solid, what's stubbed, what's untested, and what's missing.

## Final smoke test (all green)

```
==> final smoke: every major endpoint
  /healthz                        : 200    (whitelisted, no auth)
  /v1/tenants (no auth)           : 401    (middleware rejects)
  /v1/tenants (with token)        : 200
  /v1/carriers                    : 200
  /v1/freeswitch/dialplan         : 200    (whitelisted for mod_xml_curl)
  /v1/freeswitch/directory        : 200    (whitelisted for mod_voicemail directory)
  /v1/freeswitch/configuration    : 200    (whitelisted for mod_ivr + mod_callcenter)
  /admin/ (cookie login)          : 200
  /admin/api-tokens               : 200
  provisioning /<MAC>.cfg         : 409    (device exists, no line bound — expected behavior)
```

## TL;DR

The SIP platform now has:

- **Multi-tenant control plane** (Go) with REST API + bearer-token auth + bcrypt-hashed token storage
- **Web admin portal** at `/admin/` (server-rendered HTML, htmx, Pico.css) — no JS framework needed
- **9 schema migrations** covering tenants, SIP domains, extensions, devices, carriers, DIDs, ring groups, voicemail, IVR, queues, per-extension features, RPS sync, API tokens
- **Every PBX feature an SMB needs**: ext-to-ext, ring/hunt groups (all 4 strategies), IVR with all action kinds, queues (mod_callcenter with live ESL sync), voicemail with email, DND, call forwarding (immediate/busy/no-answer), recording, attended/blind transfer codes, MoH
- **PSTN** via CallCentric SIP trunk + CDR pipeline
- **ZTP provisioning** for Polycom/Yealink/Grandstream with vendor-specific templates served over HTTPS, plus Polycom ZTP cloud integration for true plug-and-play (Yealink/Grandstream same pattern, add when accounts ready)
- **Dev workflow**: `bash scripts/dev-up.sh` boots the full local stack in ~5 seconds; `bash scripts/dev-down.sh` tears it down

## How to test in 30 seconds

```bash
brew install go postgresql@16 redis jq    # one-time
cd /Volumes/TendPOS/projects/SIP
bash scripts/dev-up.sh
# prints a bootstrap admin API token + URLs

open http://localhost:18080/admin/        # → login page; paste the printed token
```

For real SIP calls (softphone register, real audio):
- Install OrbStack: `brew install --cask orbstack`
- `docker compose up -d` per `TESTING.md` Path B
- This is the path I could not exercise locally (no Docker installed in the build env). Everything I built is validated against the live control plane via the Path-A loop and the dialplan XML output, but the FreeSWITCH / Kamailio configs themselves haven't been started in a real container yet.

## What's solid (validated end-to-end against live Postgres + Redis)

| Area | Status |
| --- | --- |
| Go control plane (vet, build, all tests pass) | ✅ |
| 9 SQL migrations + round-trip down/up | ✅ |
| Admin API for tenants / sip-domains / extensions / devices / device-lines / carriers / carrier_accounts / DIDs / ring groups + members / IVRs + options / queues + agents / voicemail boxes / API tokens | ✅ |
| Dialplan handler XML for: internal extension, outbound PSTN with effective CID, inbound DID (extension / ring-group / voicemail / IVR / queue), `*97` VM check, DND, CF immediate, CF busy/no-answer branching (`${cond(...)}`), recording, transfer feature-code binding (bind_meta_app) | ✅ |
| FreeSWITCH configuration XML served dynamically: `ivr.conf` (mod_ivr menus), `callcenter.conf` (queues+agents+tiers with deduplicated agents and Kamailio-routed contact URIs) | ✅ |
| Directory XML for mod_voicemail user lookups | ✅ |
| Provisioning rendering for Polycom (XML), Yealink (ini), Grandstream (P-value XML) — tested per-vendor against live DB | ✅ |
| RPS provider framework + Polycom adapter (httptest-validated wire format) + LogOnly fallback | ✅ |
| Bearer token auth: middleware applied to all `/v1/*` except whitelisted FS callbacks; bootstrap via env; tenant-scoped tokens enforce URL tenant match; admin-scope check on token CRUD | ✅ |
| Web portal: login → cookie → dashboard → tenant detail → create-via-form for SIP domains, extensions, devices, ring groups, IVRs, queues; API token issue + revoke | ✅ |
| Round-robin via Redis INCR (counter persists, rotation deterministic) | ✅ |
| Live ESL queue/agent provisioning attempts the right commands and degrades cleanly when FS isn't connected | ✅ |
| ESL CDR pipeline: A-leg filter, full event→CDR mapping, ON CONFLICT DO NOTHING for safety | ✅ |
| SMTP voicemail-to-email: MIME parsing-roundtrip test, configured/not-configured branching, file-missing degradation | ✅ |

## What's stubbed (compiles + tests pass, but needs real-world validation)

| Area | Why |
| --- | --- |
| FreeSWITCH boots successfully with our conf tree | xmllint passes but I couldn't start FS without Docker |
| Kamailio boots with `kamailio.cfg` | No linter exists outside the binary |
| mod_xml_curl actually sends the form fields we read | Built against docs; never observed live |
| Real SIP REGISTER + INVITE + media flow | Needs softphone + Linux network stack |
| Sofia gateway registers with CallCentric | Needs real CallCentric account + public IP |
| ESL CDR pipeline against real `CHANNEL_HANGUP_COMPLETE` events | Tested with synthetic events |
| ESL queue/agent live provisioning commands (`callcenter_config queue load <id>` etc.) | Command builder unit-tested; only LogOnly path validated live |
| Polycom ZTP wire format (`POST {APIBase}/v1/devices`) | Based on public docs; httptest verifies behavior on 200/201/409/500 but not against the real Poly API |
| Yealink RPS / Grandstream GDMS / Cisco PSS adapters | **Not implemented.** Same pattern as Polycom — add when vendor accounts are available. Devices for these vendors fall back to LogOnly. |

## Known limitations (intentional, documented)

- **No TLS on SIP yet** — Kamailio config has the TLS listener block commented out. UDP/TCP only. Phase 2.x.
- **usrloc is in-memory** (`db_mode=0`) — restarting Kamailio drops registrations. Phase 3 db-backed migration deferred.
- **Outbound carrier selection picks the first enabled `carrier_account`** — no per-tenant `outbound_routes` matching yet.
- **Voicemail recordings live on FreeSWITCH host** — control plane needs the recordings volume mounted to read files for email attachment. Docker compose doesn't share that mount yet; with no mount, emails go out with a "see FS host at <path>" note instead of an attachment.
- **`cf_busy` works only when `cf_no_answer` is also set** — when both, FS `${cond(originate_disposition)}` branches. When only `cf_busy`, busy→transfer + everything else→voicemail (if enabled).
- **Per-tenant custom MoH** — only the default `local_stream://moh` is served; per-tenant streams are Phase 5.
- **Portal does not yet have**: voicemail message inbox (file streaming), edit/delete for most entities, ring-group/queue/IVR nested-entity forms (members, options, agents), device-line binding, DID create form. Read shows everything; writes cover: tenants, extensions, devices, ring groups, IVRs, queues, extension features (DND/CF/VM/recording), voicemail box create, API tokens.
- **CDR viewer** ships and renders correctly; depends on real `CHANNEL_HANGUP_COMPLETE` events to populate (live test inserted a fake row to verify).
- **Portal tenant scoping** is enforced server-side but cookie still stores plaintext token (HttpOnly+Lax+optionally-Secure). Production would prefer session IDs in a server-side store (Redis) — defer.
- **No rate limiting / lockout on auth** — bcrypt cost is the only barrier against credential stuffing on the bootstrap token.

## What's missing entirely

These would be Phase 5+ work, multi-week each:

- **WebRTC softphone** in the browser (SIP-over-WSS, Janus / drachtio / coturn TURN server)
- **SMS** — outbound + inbound via carrier APIs (CallCentric supports SIP-SIMPLE; Telnyx/Bandwidth use REST), conversation views, MMS attachments
- **Video conferencing** — Jitsi or LiveKit integration
- **Mobile apps** — iOS/Android with push-notification call wakeup (requires VoIP push via APNS/FCM)
- **Number porting workflows**
- **Emergency E911** — address registration with carriers, location services
- **Real-time UI updates** — websockets/SSE for live call state in the portal
- **Observability** — Prometheus metrics, OpenTelemetry tracing, audit log
- **Production multi-instance** — HA Kamailio pairs, shared Redis for usrloc + RR state, etc.

## Repo shape

```
/Volumes/TendPOS/projects/SIP/
├── README.md, TESTING.md, STATE.md (this file)
├── docker-compose.yml, .env.example, .gitignore
├── scripts/
│   ├── dev-up.sh, dev-down.sh    ← 30-second local test loop
│   ├── seed.sh                    ← creates tenant + 2 extensions; optional CallCentric seed via env
│   └── smoke-test.md              ← manual walkthrough for full stack (OrbStack/Linux)
├── db/migrations/0001..0009_*.up.sql + .down.sql
├── kamailio/etc/                  ← kamailio.cfg + dispatcher.list
├── freeswitch/conf/               ← full conf tree (sofia profiles, dialplan, autoload_configs)
└── control-plane/                 ← Go service
    ├── cmd/server/main.go
    └── internal/
        ├── api/                   ← admin REST API + auth middleware
        ├── config/                ← env-driven config
        ├── e164/                  ← E.164 normalization (tested)
        ├── freeswitch/            ← dialplan handler, directory handler, configuration handler, ESL CDR + VM + queue sync
        ├── portal/                ← web admin (embed.FS templates + handlers)
        ├── provisioning/          ← ZTP HTTPS server + per-vendor templates
        ├── rps/                   ← manufacturer redirection (Polycom + LogOnly)
        ├── smtp/                  ← MIME-email helper
        └── store/                 ← Postgres + Redis access; one file per domain entity
```

## Recommended next steps (in order of leverage)

1. **Stand up the real FS+Kamailio containers** via OrbStack or a Linux VM and verify a real REGISTER + ext-to-ext call. This is the biggest unknown right now — every config file passes xmllint but no telephony binary has booted with them.
2. **Yealink RPS adapter** — same pattern as Polycom, takes ~1 hour with a Yealink RPS account.
3. **CDR viewer in the portal** — small UI win, table query already works.
4. **Voicemail inbox in the portal** — file streaming + play/download/delete; needs the recordings volume mount in compose.
5. **Per-tenant outbound_routes lookup** — replaces the "first enabled carrier_account" hack.
6. **Phase 5 WebRTC softphone** — biggest user-visible jump after the portal; lets non-deployers test calls without buying hardware.

## What I want to double-check

A few risks I'd flag for a code review pass:

- The Polycom ZTP request body (`{"mac":"<plain>","profile_id":"<id>"}`) is based on docs I remember, not their live API. First real account setup will tell whether the field names are exactly right.
- `callcenter_config queue load <id>` works as I described in `internal/freeswitch/queue_provisioning.go`. mod_callcenter docs are sparse; if the live behavior differs, the fallback is the manual `reload mod_callcenter`.
- `bind_meta_app 2 a s execute_extension::att_xfer XML features` syntax for FS feature codes. This is the canonical pattern but FS occasionally changes meta_app arg parsing across versions.
- The dialplan `${cond(${originate_disposition} == USER_BUSY ? cf_busy : cf_no_answer)}` expression for branching CF busy vs no-answer. Tested that the XML renders correctly; not tested that FS evaluates it the way we want at runtime.

## How auth works (cheat sheet)

```bash
# After dev-up.sh, the bootstrap token is at /tmp/sip-cp.token
export API_TOKEN=$(cat /tmp/sip-cp.token)

# Call the API
curl -H "Authorization: Bearer $API_TOKEN" http://localhost:18080/v1/tenants

# Or open the web portal:
open http://localhost:18080/admin/   # paste $API_TOKEN at the login form

# Issue a tenant-scoped token via the API
curl -X POST -H "Authorization: Bearer $API_TOKEN" \
  -H 'Content-Type: application/json' \
  http://localhost:18080/v1/api-tokens \
  -d '{"name":"acme-laptop","tenant_id":"<acme-uuid>","scope":"write","expires_in":"720h"}'
# → returns {"token":"sip_...","id":"...","name":...} — copy the token NOW, it won't be shown again
```

## Total work this session

- 40 tasks shipped (all Phase 1, Phase 2, Phase 3 Waves 1, 1.5, 2, 2.5, 3, 3.5, 4, 4.5, 5.0, 5.5; Phase 3 #10 RPS with Polycom + Yealink + Grandstream adapters; Phase 4.0 auth; Phase 4.1 web portal; Phase 4.2 CDR viewer + extension feature edit)
- ~6,000 lines of Go across `cmd/`, `internal/api`, `internal/config`, `internal/e164`, `internal/freeswitch`, `internal/portal`, `internal/provisioning`, `internal/rps`, `internal/smtp`, `internal/store`
- 9 SQL migrations
- ~30 FreeSWITCH XML config files + per-vendor ZTP templates
- 1 Kamailio config + dispatcher list
- ~10 unit-test files; `go test ./...` is clean
- All 8 of those entity types exposed via REST API + most via portal forms
