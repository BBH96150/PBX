# Testing

The control-plane has two test layers: **unit** (no external services, runs
everywhere) and **integration** (needs a real Postgres, gated behind a build
tag). This split exists because local Postgres can't run in every dev
environment (kernel `SHMALL` limits) — so the default `go test ./...` must stay
database-free, and the heavier coverage runs in CI against a throwaway DB.

## Running tests

```bash
cd control-plane

# Unit tests — fast, no DB. This is what the CI "Build & Test" job runs.
go test ./...
go test -race -count=1 ./...        # as CI runs it

# Integration tests — needs a migrated Postgres. Mirrors the CI "integration" job.
export DATABASE_URL='postgres://sip:sip@127.0.0.1:5432/sipplatform?sslmode=disable'
# (apply migrations first: db/migrations via golang-migrate)
go test -tags=integration -count=1 ./...
```

The standard quality bar before committing:

```bash
go build ./... && go vet ./... && go test ./...
```

## Layout

| Layer | Build tag | Needs DB | Where it runs |
|---|---|---|---|
| Unit | none | no | every `go test ./...`, CI **Build & Test** |
| Integration | `//go:build integration` | yes (`DATABASE_URL`) | CI **Integration tests (postgres)** |

Unit tests cover pure logic: crypto seal/open, config env parsing, e164
normalization, webhook signing + SSRF guard, SSO PKCE/SAML helpers, audit
request parsing, FreeSWITCH dialplan/SDP/queue builders, portal helpers
(WS-URL, CSV-safe, markdown KB rendering), and store digest helpers.

Integration tests cover the things that only make sense against real SQL: store
CRUD + dialplan lookups (tenants, contacts, webhooks, extensions, ring groups,
queues, IVRs, paging groups) with cross-tenant guards, the `/v1` HTTP API
contract (bearer auth, scope, create→list round-trips), and the admin portal
HTTP surface (session cookie auth, page rendering, the Help Center).

## CI

`.github/workflows/ci.yml` runs these jobs on every push/PR:

- **Lint** — golangci-lint.
- **Build & Test** — `go build` + `go test -race -count=1 ./...` (unit only).
- **Migrations apply cleanly** — `migrate up` on a fresh Postgres.
- **Integration tests (postgres)** — Postgres service + `migrate up` +
  `go test -tags=integration -count=1 ./...`.
- **govulncheck** — stdlib/dependency CVE scan.

The **docker** build (and therefore **deploy**) `needs` lint, test, migrations,
and **integration** — a failing integration test blocks the release.

## Writing a DB-backed test

Add the build tag and use the existing helpers:

```go
//go:build integration

func TestThing(t *testing.T) {
	s := testStore(t)                 // skips if DATABASE_URL unset
	ten := makeTenant(t, s)           // unique slug, cascade-cleaned at end
	ext, domain := makeExtension(t, s, ten, "1001")
	// ... exercise store methods; assertions ...
}
```

`testStore` builds `&store.Store{DB: pool}` straight from `DATABASE_URL` (no
Redis needed for store/HTTP tests). `makeTenant`/`makeExtension` register
`t.Cleanup` that cascade-deletes the tenant, so tests don't leak rows. The HTTP
layers (`internal/api`, `internal/portal`) construct the real router over that
store and drive it with `httptest`.

When `DATABASE_URL` is unset the integration files compile but every test
`t.Skip`s — so `go test -tags=integration ./...` is safe to run locally even
without a database.
