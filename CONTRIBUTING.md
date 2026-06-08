# Contributing

This repo is a multi-tenant SIP/UCaaS platform (Kamailio + FreeSWITCH + Go
control-plane + Postgres + Redis + Caddy). See [`README.md`](README.md) for the
architecture and the per-component READMEs for each piece.

## Workflow

- Work on a branch; open a PR into `main`. Pushing to `main` triggers CI and,
  if green, an automatic deploy to the production box.
- Keep commits focused and well-described. Co-author trailer for AI-assisted
  commits is fine.

## Quality bar

Before pushing control-plane changes:

```bash
cd control-plane
go build ./... && go vet ./... && go test ./...
```

CI enforces lint, unit tests (with `-race`), clean migrations, integration
tests against a real Postgres, and `govulncheck`. The deploy is gated on all of
them — see [`docs/testing.md`](docs/testing.md).

## Where things live

| Change | Touch |
|---|---|
| REST API (`/v1`) | `control-plane/internal/api` — and update [`docs/openapi.yaml`](docs/openapi.yaml) + the embedded copy `control-plane/internal/api/openapi.yaml` |
| Admin portal pages | `control-plane/internal/portal` (handlers + `templates/*.html` + `static/`) |
| Domain data / SQL | `control-plane/internal/store` + a new numbered migration pair in `db/migrations/` |
| FreeSWITCH behavior | `control-plane/internal/freeswitch` (dialplan/directory/config are served dynamically via xml_curl — there is no static dialplan) |
| Customer docs | [`docs/kb/`](docs/kb/) — rendered in-app at `/admin/help`; keep `control-plane/internal/portal/kbcontent/` in sync |

## Migrations

Add an up/down pair under `db/migrations/` with the next number
(`00NN_name.up.sql` / `.down.sql`). They run via golang-migrate locally, in CI,
and on deploy. **Local Postgres may not run in every dev environment** — if so,
validate SQL via the CI "Migrations apply cleanly" job rather than locally.

## Config / infra gotchas (read before touching infra)

- **Kamailio and FreeSWITCH configs are NOT synced by CI** and the box copies
  have **diverged** from the repo (notably: the box `kamailio.cfg` holds the
  **real DB password**; the repo carries a placeholder). Never blindly `scp` the
  repo config over the box copy — use the `ops-*` workflows, which inject
  secrets from the container env. See `kamailio/README.md`.
- Only the **control-plane** container is image-deployed + recreated by CI.
  Kamailio/FreeSWITCH/Caddy/rtpengine changes are applied through the ops
  workflows (`.github/workflows/ops-*.yml`), several of which restart a service
  (production-affecting — they're manual `workflow_dispatch`).
- Production-affecting or hard-to-reverse actions (e.g. restarting Kamailio,
  which drops registrations) should be deliberate and verified.

## Secrets

Real secrets live only on the box (gitignored `.env`, `secrets/*.pem`). The repo
and the committed compose files contain only `${VAR}` placeholders. Do not
commit secrets or pull production data (PII) off the box.
