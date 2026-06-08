# kamailio

The SIP edge / registrar that sits in front of FreeSWITCH. It is the first hop
for every phone and softphone and the SIP-over-WebSocket entry point for browser
clients.

## Responsibilities

- **REGISTER** — digest-authenticates phones against the Postgres `subscriber`
  view (`auth_db`, precomputed HA1), then stores the AOR binding in the `location`
  usrloc table. AORs are `user@domain` (multi-tenant: `use_domain=1`).
- **INVITE from a phone** — authenticates, then dispatches to the FreeSWITCH pool
  (`dispatcher` set 1, `dispatcher.list`).
- **INVITE from FreeSWITCH** — trusted by source (the FS dispatcher host / Docker
  bridge fallback), looks up the callee AOR in usrloc, and `t_relay`s.
- **SIP-over-WebSocket** — listens plain `ws` on `:5066`; Caddy terminates `wss`
  on `:443/ws*` and proxies the upgrade here. WS/WSS legs get NAT handling
  (`force_rport`, `fix_nated_register`, contact aliasing) and are bridged to
  FreeSWITCH media through **rtpengine** (`ng` socket `udp:rtpengine:22222`).
  Desk-phone UDP/TCP calls never touch rtpengine.
- **Anti-flood** via `pike`; basic SIP sanity / max-forwards checks.

## Key files

| File | Purpose |
| --- | --- |
| `etc/kamailio.cfg` | The entire config: globals, module load/params, and `request_route` routing logic. |
| `etc/dispatcher.list` | The FreeSWITCH pool (set `1` → `sip:freeswitch:5080`, weighted, `duid=fs1`). |

Listeners: UDP/TCP `5060` (SIP), TCP `5066` (WS). TLS `5061` is present but
commented (TLS is terminated at Caddy for WSS today).

## Configuration notes

- `DBURL` is `#!define`'d at the top of `kamailio.cfg`. Module DB params
  (`usrloc`, `auth_db`, `uri_db`) all reference it.
- `WITH_USRLOCDB` toggles usrloc persistence: `0` = in-memory (current),
  `1` = DB-backed `location` table (needed before running multiple Kamailio
  instances). The `ops-kamailio` workflow can flip this safely with auto-rollback.
- `auth_db` uses `calculate_ha1=0` with `password_column="ha1"` — the schema
  stores precomputed HA1, so the realm/domain must match exactly or auth fails.
- `dispatcher` pings FreeSWITCH with OPTIONS and probes dead nodes; a `rtimer`
  route re-checks the dispatcher set.

## Run / test locally

Started by the root compose as the `kamailio` service
(`kamailio/kamailio-ci:5.5.2-alpine`), mounting `kamailio/etc` read-only. In dev
it publishes its ports; in production it should run with host networking on Linux.

```bash
docker compose up -d kamailio
docker exec sip-platform-kamailio-1 kamcmd ul.dump   # who's registered now
docker exec sip-platform-kamailio-1 kamcmd dispatcher.list
```

There is no unit-test harness for the config; validate behavior against real
REGISTER/INVITE traffic (or `kamailio -c` for a config syntax check).

## Deploy

> **CRITICAL:** `ci.yml` does **NOT** sync `kamailio/etc/` to the box. The box's
> live `kamailio.cfg` carries the **real DB password** injected at deploy time;
> the repo copy carries a placeholder. **Never blindly overwrite the box copy** —
> doing so will break SIP auth (the password reverts to the placeholder).

Apply config changes to the box through the dedicated workflows:
- `restart-sip.yml` — optionally syncs conf dirs (destructive; overwrites
  server-side edits) and restarts.
- `ops-kamailio.yml` — read-only usrloc/`location` diagnostics, and the guarded
  `WITH_USRLOCDB` flip/rollback.

## Gotchas

- **`MANDATORY_IE_MISSING` in FS logs** usually means Kamailio challenged FS for
  auth because the `FROM_FREESWITCH` source-trust route didn't match — the FS
  source IP must be in the dispatcher list or the Docker-bridge fallback.
- **Auth fails after a domain rename** — the realm in the client's Identity must
  equal the tenant `sip_domain` exactly (HA1 is realm-bound).
- WebRTC media only works when rtpengine is up and reachable on
  `udp:rtpengine:22222` and the public media IP is advertised correctly.
