# Control-plane API & Webhooks

Integration reference for the SIP platform's REST API (`/v1`) and outbound
webhooks. Base URL in production: `https://pbx.tendpos.com`.

## Authentication

All `/v1` endpoints (except the public ones noted below) require a bearer token:

```
Authorization: Bearer sip_<token>
```

Tokens are **tenant-scoped** or platform-wide (super-admin). A tenant admin can
self-issue tenant-scoped keys in the portal at **Tenant → API keys**
(`/admin/tenants/{id}/api-keys`), choosing a scope:

- `read` — GET endpoints only
- `write` — read + create/update/delete
- `admin` — full control (token/user/invite management)

`401` = missing/invalid token; `403` = token lacks the required scope or
addresses a different tenant.

Public (no auth): `GET /v1/version`, `POST /v1/signup`,
`POST /v1/invites/accept`, `POST /v1/password-reset/{request,confirm}`.

## Read endpoints (integration pull)

| Method & path | Returns |
|---|---|
| `GET /v1/tenants` | tenants you can see |
| `GET /v1/tenants/{id}` | one tenant |
| `GET /v1/tenants/{id}/extensions` | active extensions |
| `GET /v1/tenants/{id}/cdrs` | call records (see filters) |
| `GET /v1/tenants/{id}/contacts` | directory contacts (`?q=` search) |
| `GET /v1/tenants/{id}/dids` | phone numbers |
| `GET /v1/tenants/{id}/ring-groups` | ring groups |
| `GET /v1/tenants/{id}/queues` | call queues |
| `GET /v1/tenants/{id}/paging-groups` | paging / PTT groups (with member counts) |
| `GET /v1/tenants/{id}/devices` | provisioned devices (provisioning token omitted) |
| `GET /v1/devices/{mac}` | a provisioned device |
| `GET /v1/carriers` | available carriers |
| `GET /v1/extensions/{id}/voicemail` | a voicemail box |
| `GET /v1/extensions/{id}/voicemail/messages` | voicemail messages (metadata; no audio path) |

**CDR filters** (`GET …/cdrs`): `limit` (default 100, max 10000),
`since`/`until` (RFC3339), `direction` (`inbound|outbound|internal`), `q`
(substring on from/to/caller-id). Each CDR includes `note` (the editable
call note/tag).

## Write endpoints (integration push)

| Method & path | Body |
|---|---|
| `POST /v1/tenants/{id}/contacts` | `{name, number, company?, notes?}` |
| `DELETE /v1/tenants/{id}/contacts/{contactID}` | — |
| `POST /v1/tenants/{id}/extensions` | `{sip_domain_id, extension, sip_username, sip_password, display_name}` |
| `PATCH /v1/extensions/{id}/features` | any of `{do_not_disturb, cf_immediate, cf_busy, cf_no_answer, voicemail_enabled, recording_enabled}` |
| `POST /v1/tenants/{id}/dids` | DID + destination |
| `POST /v1/tenants/{id}/ring-groups`, `/queues`, `/ivrs` | create call-handling entities |

(Full create surface also covers devices, sip-domains, carrier accounts,
ring-group members, IVR options, queue agents — see `internal/api/server.go`.)

## Outbound webhooks (event push)

Configure per tenant in the portal at **Tenant → Webhooks**
(`/admin/tenants/{id}/webhooks`). Each endpoint has an HTTPS URL, a generated
signing secret, and an optional event subscription list (empty = all events).
The portal shows each endpoint's last delivery status.

**Events:** `call.completed`, `trunk.down`, `trunk.up`, `voicemail.new`
(plus `test.ping` from the Send-test button).

**Request:** `POST` with JSON body:

```json
{ "event": "call.completed", "sent_at": "2026-06-07T01:23:45Z", "data": { ... } }
```

Headers:

- `X-Webhook-Event: call.completed`
- `X-Webhook-Id: <endpoint uuid>`
- `X-Webhook-Signature: sha256=<hex>` — HMAC-SHA256 of the **raw request body**
  using the endpoint's signing secret.

Delivery is best-effort with a 6s timeout; redirects are not followed; only
public HTTPS URLs are allowed (internal/loopback/private addresses are refused).

**Verify the signature (pseudocode):**

```
expected = "sha256=" + hex(hmac_sha256(secret, raw_body))
if not constant_time_equals(expected, header["X-Webhook-Signature"]):
    reject(401)
```

### Event payloads (`data`)

- **call.completed** — `call_uuid, direction, from, to, caller_id_num, caller_id_name, started_at, duration_sec, billable_sec, disposition, hangup_cause`
- **trunk.down / trunk.up** — `trunk, carrier, gateway, prev_state, state`
- **voicemail.new** — `extension_id, user, domain, caller_id_num, caller_id_name, duration_sec`
