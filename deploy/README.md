# Deploy & prod compose

## How prod is deployed

Pushing to `main` runs `.github/workflows/ci.yml`. On success it runs the
**Deploy to Hetzner** job, which SSHes into the box (`root@5.78.207.2`) with the
`CI_DEPLOY_*` secrets and:

1. Reclaims disk (prunes images/build cache).
2. `rsync`s `db/migrations/` to the box.
3. `rsync`s `docker-compose.prod.yml` → the box's `docker-compose.override.yml`
   (the repo-controlled, **additive** override layer — currently the
   `freeswitch_storage` volume the voicemail inbox needs).
4. Pulls the `control-plane` image, runs `migrate up`, recreates `control-plane`.
5. Smoke-checks `http://127.0.0.1:8080/healthz` on the box.

The box's **base** `docker-compose.yml` is hand-maintained and is **not** synced
from the repo. `deploy/prod-compose.snapshot.yml` is a version-controlled,
secret-free capture of it (see that file's header for the divergences).

## Ops

`.github/workflows/ops-disk.yml` (manual `workflow_dispatch`) operates the box
over the CI SSH key — `clean`, `harden`, `recreate`, `show_compose`,
`apply_vm_override` inputs. The Hetzner web console can't reliably paste, so use
this instead of the console.

`.github/workflows/monitor.yml` (scheduled, every 30 min) checks disk %,
container health, and `/healthz`; a failed run emails repo admins.

### Security posture (audited 2026-06-03)

- **Firewall:** `ufw` is active with a default-deny allowlist — only SSH (22),
  SIP/RTP (5060, 5066, 5070, 16384:16484) and Caddy (80/443) are open. Run
  `ops-disk.yml -f audit_net=yes` to re-audit listening ports + firewall.
- **Redis:** now bound to `127.0.0.1:6379` (was published to `0.0.0.0` on a
  random port — ufw-blocked, but a single point of failure; fixed via
  `ops-disk.yml -f fix_redis_bind=yes`).
- **Go stdlib:** toolchain pinned to 1.25.11 (patches GO-2026-5039 / 5037).
- **ESL:** rotated off the default `ClueCon` (see below).
- **OPEN RECOMMENDATION — provisioning auth:** the provisioning server (`:8443`)
  serves device configs (which contain SIP credentials) **by MAC alone, no
  token**. Currently mitigated because `8443` is ufw-blocked (not internet-
  reachable). Before exposing provisioning for over-the-internet ZTP, gate the
  config fetch on the per-device `provisioning_token` and propagate it through
  the RPS redirect — needs testing against real handsets, so it wasn't changed
  blind.

### Credentials / ESL

`ops-disk.yml -f check_env=yes` reports (status only, never values) whether the
box's `.env` overrides the weak compose defaults. Prod uses a strong
`POSTGRES_PASSWORD` and (since 2026-06-02) a rotated `ESL_PASSWORD`.

The FreeSWITCH ESL password lives as a **literal** in the box's
`freeswitch/conf/autoload_configs/event_socket.conf.xml` (the `$${var:-default}`
form FS provides isn't reliable) and as `ESL_PASSWORD` in `.env` (the
control-plane connects with that). Rotate both atomically with
`ops-disk.yml -f rotate_esl=yes` — it generates a fresh secret, updates both,
force-recreates FS + control-plane, verifies the control-plane reconnects, and
auto-rolls-back if not. The FS container healthcheck is overridden to a
password-free netstat probe (`docker-compose.prod.yml`) so rotation doesn't
flap the health status.

## Base-compose reconciliation (pending, do supervised — lower priority now)

Goal: make the base compose repo-controlled so changes deploy automatically,
instead of drifting on the box.

**Priority note:** the additive `docker-compose.prod.yml` override (synced by the
deploy) already covers most ongoing compose needs (volumes, healthchecks, etc.),
so the marginal value of a full base swap is now lower. The remaining gap is
base-level deltas (image vs build, SAML mounts, redis bind, missing
`SIP_DOMAIN_SUFFIX`/`fs-gateways-init`). `prod-compose.snapshot.yml` is the
current accurate base reference.

This is **not** a no-op (the box base is missing `SIP_DOMAIN_SUFFIX` and
`fs-gateways-init` that the repo base has), and a full base swap replaces the
live compose, so do it with a human watching and validate first. Suggested
steps:

1. Decide the canonical prod compose: either (a) a standalone
   `deploy/prod-compose.yml` (start from `prod-compose.snapshot.yml`, fold in the
   missing `SIP_DOMAIN_SUFFIX` env + `fs-gateways-init` + the `freeswitch_storage`
   mounts so the override is no longer needed), or (b) repo base +
   `docker-compose.prod.yml` override using `-f`.
2. **Validate before applying:** copy the candidate to a temp dir on the box and
   `docker compose -f <candidate> config` ; diff against
   `docker compose -p sip-platform config` (the current effective config). Expect
   only the intended additions (SIP_DOMAIN_SUFFIX, fs-gateways-init). Confirm the
   box's `.env` provides `SIP_DOMAIN_SUFFIX` (currently the container doesn't get
   it — wildcard sip_domain auto-gen relies on it).
3. Back up the box's current `docker-compose.yml`.
4. Switch the `ci.yml` deploy to rsync the canonical file and use it; deploy;
   watch `monitor.yml` / `/healthz`. Roll back by restoring the backup if needed.
