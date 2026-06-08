# Outbound Webhooks

Webhooks push a signed JSON payload to a URL you control whenever events happen in your workspace — so your own systems can react in real time. Find them under **Admin ▾ → Webhooks**.

## Add an endpoint

1. Go to **Admin ▾ → Webhooks**.
2. In **Add an endpoint**, enter:
   - **Endpoint URL** — must be a public **HTTPS** URL (internal/private addresses are blocked).
   - **Events** — check the specific events you want. Leave all unchecked to receive every event.
3. Click **Add webhook**. A signing secret is generated automatically and shown in the endpoints table.

## Verify it's really us

Each request includes an `X-Webhook-Signature: sha256=…` header — an HMAC-SHA256 of the raw request body using your endpoint's signing secret. Compute the same HMAC on your side and compare to confirm the payload is genuine and untampered.

> Delivery is best-effort; failed deliveries are not retried.

## Test, monitor, and manage

In the endpoints table you can:

- **Send test** — fire a sample delivery to confirm your receiver works.
- See **Last delivery** — an **ok** or **fail** status with a timestamp (hover a failure to see the error).
- **Enable** / **Disable** the endpoint.
- **Rotate secret** — generates a new signing secret (update your receiver, or deliveries will fail verification).
- **Delete** the endpoint.

## Related

- [API keys](integrations-api-keys.md)
- [Trunk-down alerts and the daily digest](integrations-alerts-and-digest.md)
