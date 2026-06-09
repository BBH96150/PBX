# Integration examples

Runnable starting points for integrating with the platform.

## Webhook receivers

Verify the HMAC-SHA256 signature on every delivery, then handle events
(`call.completed`, `trunk.down`, `trunk.up`, `voicemail.new`, `test.ping`).

- **[webhook-receiver.py](webhook-receiver.py)** — Flask. `pip install flask`,
  then `WEBHOOK_SECRET=… python3 webhook-receiver.py`.
- **[webhook-receiver.go](webhook-receiver.go)** — stdlib only.
  `WEBHOOK_SECRET=… go run webhook-receiver.go`.

Both listen on `:8088/webhook`. To receive real deliveries:

1. Expose the server over **public HTTPS** (e.g. `ngrok http 8088`) — the
   platform only delivers to public HTTPS URLs and does not follow redirects.
2. In the portal, **Tenant → Webhooks**, add the endpoint with that HTTPS URL.
   Copy the **signing secret** into `WEBHOOK_SECRET`.
3. Use the **Send test** button — you should see `test ping received`.

See also the [webhooks KB article](../kb/integrations-webhooks.md) and the
[API quickstart](../api-examples.md).

## REST API

Curl recipes for auth, reads, writes, and signature verification are in
[api-examples.md](../api-examples.md). The full reference is the interactive
Swagger UI at `https://pbx.tendpos.com/v1/docs`.
