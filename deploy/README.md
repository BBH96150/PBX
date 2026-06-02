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

## Base-compose reconciliation (pending, do supervised)

Goal: make the base compose repo-controlled so changes deploy automatically,
instead of drifting on the box. This is **not** a no-op (the snapshot is missing
`SIP_DOMAIN_SUFFIX` and `fs-gateways-init` that the repo base has), so it must be
done with a human watching. Suggested steps:

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
