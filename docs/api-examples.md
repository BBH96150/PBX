# API quickstart (curl)

Copy-paste recipes for the `/v1` REST API. For the full, browsable reference see
the interactive docs at **`https://pbx.tendpos.com/v1/docs`** (Swagger UI) or the
raw spec at [`openapi.yaml`](openapi.yaml) / `https://pbx.tendpos.com/v1/openapi.yaml`.

## Authentication

Every `/v1` call (except the public ones) needs a bearer token. Create a
tenant-scoped key in the portal at **Tenant → API keys** (`/admin/tenants/{id}/api-keys`),
choosing a scope: `read`, `write`, or `admin`.

```bash
export BASE=https://pbx.tendpos.com
export TOKEN=sip_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
auth=(-H "Authorization: Bearer $TOKEN")
```

`401` = missing/invalid token; `403` = token lacks the scope or addresses a
different tenant.

## Read

```bash
# Who am I / is it up (public, no token)
curl -s $BASE/v1/version

# Tenants you can see
curl -s "${auth[@]}" $BASE/v1/tenants

# One tenant
curl -s "${auth[@]}" $BASE/v1/tenants/$TENANT

# Active extensions
curl -s "${auth[@]}" $BASE/v1/tenants/$TENANT/extensions

# Paging / PTT groups (with member counts)
curl -s "${auth[@]}" $BASE/v1/tenants/$TENANT/paging-groups
curl -s "${auth[@]}" $BASE/v1/tenants/$TENANT/paging-groups/$GROUP/members

# Ring groups / queues / DIDs / devices / contacts
curl -s "${auth[@]}" $BASE/v1/tenants/$TENANT/ring-groups
curl -s "${auth[@]}" $BASE/v1/tenants/$TENANT/queues
curl -s "${auth[@]}" $BASE/v1/tenants/$TENANT/dids
curl -s "${auth[@]}" $BASE/v1/tenants/$TENANT/devices
curl -s "${auth[@]}" $BASE/v1/tenants/$TENANT/contacts?q=ada
```

### Call records (CDRs)

```bash
# Filters: limit (default 200, max 10000), since/until (RFC3339),
# direction (inbound|outbound|internal), q (substring on from/to/caller-id)
curl -s "${auth[@]}" \
  "$BASE/v1/tenants/$TENANT/cdrs?direction=inbound&since=2026-06-01T00:00:00Z&limit=50"
```

## Write (needs `write` or `admin` scope)

```bash
# Add a directory contact
curl -s "${auth[@]}" -H 'Content-Type: application/json' \
  -d '{"name":"Ada Lovelace","number":"+14155550101","company":"Analytical"}' \
  $BASE/v1/tenants/$TENANT/contacts

# Create an extension
curl -s "${auth[@]}" -H 'Content-Type: application/json' \
  -d '{"sip_domain_id":"'$DOMAIN'","extension":"1001","sip_username":"1001","sip_password":"s3cret","display_name":"Front desk"}' \
  $BASE/v1/tenants/$TENANT/extensions

# Toggle call-handling features on an extension
curl -s "${auth[@]}" -X PATCH -H 'Content-Type: application/json' \
  -d '{"do_not_disturb":true,"cf_busy":"+14155559999"}' \
  $BASE/v1/extensions/$EXT/features
```

## Webhooks

Configure endpoints per tenant in the portal at **Tenant → Webhooks**. Each gets
a signing secret. Events: `call.completed`, `trunk.down`, `trunk.up`,
`voicemail.new` (+ `test.ping` from the Send-test button).

Delivery is a `POST` with a JSON envelope:

```json
{ "event": "call.completed", "sent_at": "2026-06-08T01:23:45Z", "data": { ... } }
```

Headers: `X-Webhook-Event`, `X-Webhook-Id`, and
`X-Webhook-Signature: sha256=<hex>` — an HMAC-SHA256 of the **raw request body**
keyed by the endpoint secret.

### Verify a webhook signature (Python)

```python
import hmac, hashlib

def verify(secret: str, raw_body: bytes, header: str) -> bool:
    expected = "sha256=" + hmac.new(secret.encode(), raw_body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(expected, header)
```

Only public HTTPS URLs are accepted (loopback/private/link-local are refused),
redirects are not followed, and delivery is best-effort with a short timeout and
a couple of retries — the portal shows each endpoint's last delivery status.
