# Documentation

All docs for the SIP / UCaaS platform, by audience.

## For customers (tenant admins & users)

- **[Help Center / Knowledgebase](kb/README.md)** — 35 task-oriented articles
  (getting started, phones, calling features, paging & broadcast, reports,
  admin & security, integrations, troubleshooting, glossary). Also rendered
  **in-app** at `/admin/help`.
- **[Onboarding runbook](onboarding.md)** — bringing a new tenant online.

## For integrators (REST API & webhooks)

- **[API reference (Swagger UI)](https://pbx.tendpos.com/v1/docs)** — interactive,
  generated from the [OpenAPI spec](openapi.yaml) (also at
  `https://pbx.tendpos.com/v1/openapi.yaml`).
- **[API quickstart (curl)](api-examples.md)** — copy-paste recipes for auth,
  reads, writes, and webhook signature verification.
- **[API & webhooks notes](API.md)** — auth model, scopes, event catalog.

## For developers & operators

- **[Architecture & call flows](architecture.md)** — components, the `xml_curl`
  control model, and end-to-end flows (registration, calls, paging, WebRTC,
  CDR/webhook pipeline).
- **[Security model](security.md)** — authentication, authorization, secrets,
  webhook signing/SSRF, and input/output safety controls.
- **[Testing](testing.md)** — the unit + integration test layers and how to run
  them.
- **[Operator runbook](runbook.md)** — deploy flow, the ops workflows, and
  incident recovery (disk, Kamailio crash-loop, paging, WebRTC).
- **[Contributing](../CONTRIBUTING.md)** — workflow, quality bar, and infra
  gotchas.
- **Component READMEs** — [root](../README.md) ·
  [control-plane](../control-plane/README.md) · [kamailio](../kamailio/README.md) ·
  [freeswitch](../freeswitch/README.md) · [db](../db/README.md) ·
  [caddy](../caddy/README.md).
- **Mobile** — [native broadcast app (Capacitor)](../mobile/paging/README.md).

> Note: the customer knowledgebase has a synced embedded copy at
> `control-plane/internal/portal/kbcontent/` (so the binary can serve it). Edit
> `docs/kb/`, then re-copy into `kbcontent/`. A CI test guards against broken
> links and an out-of-sync route map.
