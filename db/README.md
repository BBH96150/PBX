# db

PostgreSQL schema for the platform, managed as numbered
[golang-migrate](https://github.com/golang-migrate/migrate) SQL migrations.
Postgres is the durable system of record for everything: tenants, users and
memberships, extensions and SIP credentials, DIDs, carriers/trunks, ring groups,
queues, IVRs, paging groups, voicemail, CDRs, webhooks, API tokens, sessions,
audit log, and the Kamailio `subscriber`/`location` views.

(Redis holds ephemeral state — registrations, call state, presence, rate limits —
and is not migrated here.)

## Layout

```
db/migrations/
  0001_init.up.sql        0001_init.down.sql
  0002_phase2_pstn.up.sql 0002_phase2_pstn.down.sql
  ...
  0035_paging_groups.up.sql 0035_paging_groups.down.sql
```

Each migration is a matched `NNNN_name.up.sql` / `.down.sql` pair. Currently
**0001 → 0035**. Numbering is sequential and contiguous — new work appends the
next number with both an up and a down.

## How migrations are applied

- **Locally:** the `migrate` service in the root compose (under the `tools`
  profile):
  ```bash
  docker compose --profile tools run --rm migrate up
  docker compose --profile tools run --rm migrate down 1   # roll back one
  ```
- **CI:** the `migrations` job in `.github/workflows/ci.yml` spins up Postgres 16
  and runs `migrate up` against `db/migrations/` on every push/PR — this is the
  authoritative check that the schema applies cleanly.
- **Production:** the deploy job rsyncs `db/migrations/` to the box **before**
  running `migrate up` (the migrate container reads them from a host bind mount).
  If the files aren't synced first, `schema_migrations` silently stays at the old
  version — this is wired into the pipeline; don't reorder it.

## Writing a migration

1. Create `db/migrations/00NN_short_name.up.sql` and a matching `.down.sql`.
2. Make `up` forward-only and `down` a real inverse where practical.
3. Push and let CI's `migrations` job validate it against real Postgres.

> **NOTE — local Postgres can't run in some dev envs:** an ephemeral local
> Postgres may fail to start under restricted kernel limits (`SHMALL`). When you
> can't run PG locally, **validate SQL via CI** rather than locally — the
> `migrations` job is the source of truth.

## Gotchas

- The Kamailio-facing `subscriber` view stores **precomputed HA1** (auth_db runs
  with `calculate_ha1=0`); changes touching credentials must keep that column /
  realm contract intact or SIP auth breaks.
- Never edit an already-applied migration in place — add a new one. The deploy
  rsync is plain (no `--delete` on a renamed file), so renumbering shipped
  migrations causes drift between the repo and the box's `schema_migrations`.
