# Operator runbook

Day-2 operations for the production box. Pairs with
[`CONTRIBUTING.md`](../CONTRIBUTING.md) (infra gotchas) and
[`architecture.md`](architecture.md) (how it fits together).

## Deploy flow

Push to `main` → CI runs lint, unit tests, **migrations**, **integration tests
(Postgres)**, and govulncheck → on green, the **docker** job builds a multi-arch
image to GHCR (pinned by immutable digest) → the **deploy** job SSHes to the box,
rsyncs `db/migrations` + the compose override, runs `migrate up`, and
**recreates only the `control-plane` container** by digest.

What this means:
- **Control-plane code / templates / xml_curl config / migrations** ship with a
  normal push.
- **Kamailio, FreeSWITCH, and Caddy config are NOT synced or restarted by CI.**
  Apply those through the `ops-*` workflows below.

Verify a deploy: the run log should show `control-plane-1 Recreated/Started`;
then route-check (303 = exists/auth-redirect, 401 = API exists/auth-gated,
404/405 = missing) and `GET /healthz` → 200.

## Ops workflows (`.github/workflows/ops-*.yml`)

All are manual `workflow_dispatch` over the CI deploy SSH key. Several restart a
service — read the input descriptions; they spell out what's read-only vs.
production-affecting.

| Workflow | Use it to… |
|---|---|
| **ops-disk** | Diagnose/clean disk (journald, container logs, apt), install a Docker log-size cap, inspect the on-box compose. |
| **ops-env** | Report which `SMTP_*` / `ALERT_EMAIL` keys are set (status only, never values); set `ALERT_EMAIL`. |
| **ops-kamailio** | Read-only usrloc/location dump; flip `WITH_USRLOCDB` (auto-rolls-back if Kamailio doesn't return healthy); restore the pre-flip backup. |
| **ops-fs-diag** | Read-only FreeSWITCH diagnostics (mod_callcenter list formats, channels sample). |
| **ops-paging** | Install `conference.conf` + load `mod_conference`; paging discovery; `simboot` (prove static-conf fallback); live page test. |
| **ops-webrtc** | rtpengine inspect/up/verify; check `rtpengine.so` + parse Kamailio config; **kamsync** (ship `kamailio.cfg` + re-inject the real DB URL + parse-check, no restart); **kamrestart** (production); show/deploy Caddyfile. |

## Common incidents

### Disk full
`migrations rsync` or container starts fail with "No space left on device".
Run **ops-disk** (diagnose, then cleanup). A Docker log cap is installed to
prevent recurrence; logs are the usual culprit.

### Kamailio crash-loops after a config change
Symptom: `db_postgres` / `uri_db` "could not connect" in the Kamailio logs.
Cause: **the box's `kamailio.cfg` holds the real DB password; the repo file
carries a placeholder.** A blind `scp` of the repo file breaks the DB
connection.
Fix: use **ops-webrtc → kamsync** (it re-injects the real `DB_URL` from the
container env after copying), then **kamrestart**. Never copy `kamailio.cfg`
to the box without that injection. A Kamailio restart briefly drops
registrations (desk phones re-register quickly).

### Paging doesn't work / `conference list` → "command not found"
`mod_conference` didn't auto-load (its config fetch via xml_curl fails before
the control-plane is up at boot). Fix: **ops-paging → setup** ships the static
`conference.conf` and loads the module; **simboot** proves it survives a boot.

### WebRTC / live PTT has no audio
The media bridge is rtpengine. Check **ops-webrtc → verify** (container up + ng
reachability `pong`). If signaling fails, confirm Caddy proxies `/ws` → Kamailio
(**ops-webrtc → caddyshow/caddydeploy**). Media flag tuning (rtpengine
offer/answer) needs a real browser — adjust `kamailio.cfg`, then kamsync +
kamrestart.

### FreeSWITCH behaving oddly
**ops-fs-diag** for read-only diagnostics. Remember: dialplan/directory/config
are served by the control-plane via xml_curl, so many "FS" fixes are actually
control-plane changes that ship with a normal push.

## Safety rules

- Production-affecting actions (anything that restarts Kamailio/FreeSWITCH, or
  recreates all containers) should be deliberate — they can drop calls or
  registrations. The workflow inputs flag these explicitly.
- Don't pull production data (CDRs, recordings, voicemail — PII) off the box.
- Secrets live only on the box (`.env`, `secrets/`); never commit them.
