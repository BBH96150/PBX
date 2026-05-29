# SIP / UCaaS Platform

A multi-tenant SIP server and PBX, designed to grow into a full UCaaS platform comparable to RingCentral / AT&T Office@Hand.

## Status

Phase 2 complete — PSTN trunking (CallCentric) and CDR pipeline (ESL) wired. Awaiting first end-to-end validation against real phones. See [Phased roadmap](#phased-roadmap) below.

## Architecture

| Layer | Tech | Role |
| --- | --- | --- |
| Edge / SBC / signaling | **Kamailio** | TLS, SRTP, registrar (REGISTER → Redis), tenant routing by SIP domain, dispatcher to FreeSWITCH pool, anti-fraud, rate limiting |
| Media + PBX | **FreeSWITCH** | RTP/SRTP, IVR, queues, hunt groups, voicemail, recording, conferencing |
| Control plane | **Go** | Provisioning REST API, real-time dialplan via `mod_xml_curl`, event stream via ESL, zero-touch device provisioning server |
| Data | **PostgreSQL + Redis** | Postgres: tenants, users, extensions, devices, DIDs, IVR configs, CDRs. Redis: registrations, call state, presence |
| PSTN | **CallCentric** (first carrier) | SIP trunking to PSTN. Carrier abstraction layer so Telnyx/Bandwidth slot in later |

Multi-tenant from day one — `tenant_id` threads through every domain object and through SIP routing.

### Zero-touch provisioning

First-class feature. Per-vendor templates (Polycom, Grandstream, Yealink first; Cisco/Snom/Fanvil after) served from an HTTPS provisioning endpoint in the control plane. True out-of-box ZTP via manufacturer redirection services (Polycom ZTP, Grandstream GDMS, Yealink RPS, Cisco PSS) in a later phase.

## Repository layout

```
.
├── docker-compose.yml      # Local dev: postgres + redis + kamailio + freeswitch + control-plane
├── .env.example            # Copy to .env and fill in
├── kamailio/
│   └── etc/                # kamailio.cfg + included modules
├── freeswitch/
│   └── conf/               # FreeSWITCH XML config tree
├── control-plane/          # Go service
│   ├── cmd/server/         # Admin API + provisioning entrypoint
│   └── internal/
│       ├── api/            # Admin REST API
│       ├── dialplan/       # mod_xml_curl handler (FreeSWITCH dialplan over HTTP)
│       ├── esl/            # FreeSWITCH event listener
│       ├── provisioning/   # ZTP HTTPS server + per-vendor templates
│       ├── store/          # Postgres + Redis access
│       └── tenant/         # Tenant/user/extension domain logic
├── db/
│   └── migrations/         # golang-migrate SQL files
└── scripts/                # Dev helpers, smoke tests
```

## Quick start (local dev)

Prerequisites: Docker, Docker Compose. On macOS, [OrbStack](https://orbstack.dev) gives much better SIP/RTP behavior than Docker Desktop.

```bash
cp .env.example .env
docker compose up postgres redis control-plane    # core stack
# Kamailio + FreeSWITCH come online once their configs land in Phase 1 tasks 4 & 5
```

Admin API: `http://localhost:8080/healthz`
Provisioning (TLS): `https://localhost:8443/healthz`

### Networking note (macOS)

Docker Desktop on macOS does not give containers real host networking — RTP audio will not flow cleanly. For full end-to-end audio testing, deploy to a Linux VM (any cloud) or use OrbStack on macOS, which supports host networking.

## Phased roadmap

1. **Phase 1 (current)**: Repo + dev env, tenant/user/device provisioning, intra-tenant SIP calling, ZTP for Polycom/Grandstream/Yealink.
2. **Phase 2**: CallCentric trunk, inbound DID routing, outbound E.164, CDRs, carrier abstraction.
3. **Phase 3**: PBX features — IVR, hunt/ring groups, voicemail (email delivery), recording, transfer/forward. Manufacturer redirection (true ZTP).
4. **Phase 4**: Admin portal (web UI), hardened APIs, basic call analytics.
5. **Phase 5+**: WebRTC softphone, SMS, video (LiveKit), mobile/desktop apps, presence/messaging.

## Carrier(s)

Initial: **CallCentric**. The Go control plane exposes a carrier-abstracted routing layer; adding Telnyx or Bandwidth is a config + adapter change, not a rewrite.
