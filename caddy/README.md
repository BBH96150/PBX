# caddy

The public TLS front door. Caddy terminates HTTPS for the admin portal /
control-plane API and forwards SIP-over-WebSocket to Kamailio. It auto-issues and
auto-renews a Let's Encrypt certificate for the portal hostname.

## Where it sits

```
browser ──HTTPS/WSS──► Caddy :443 ──┬── /ws*      ──► kamailio:5066  (SIP over WS)
                                    └── everything ──► control-plane:8080
```

The control-plane binds `127.0.0.1:8080` on the box, so Caddy is the only way to
reach the portal/API from outside. Browser softphones can only open `wss://`, but
Kamailio listens plain `ws` on `:5066` — Caddy terminates TLS and proxies the
WebSocket upgrade through.

## Key file

- `Caddyfile` — the entire config. It:
  - serves `{$PORTAL_HOSTNAME:pbx.tendpos.com}` over auto-TLS;
  - sets security headers (HSTS, `X-Content-Type-Options`, `Referrer-Policy`);
  - enables zstd/gzip;
  - routes `handle /ws*` → `kamailio:5066` (WebSocket upgrade passthrough);
  - routes everything else → `control-plane:8080`, forwarding the original `Host`
    plus `X-Real-IP` / `X-Forwarded-For` / `X-Forwarded-Proto: https` (the portal
    relies on `Host` to keep `PORTAL_BASE_URL` link generation consistent);
  - logs to stdout in console format.

## Configuration (env vars)

| Var | Purpose |
| --- | --- |
| `PORTAL_HOSTNAME` | Hostname Caddy serves and requests a cert for. Default `pbx.tendpos.com`. |

## Requirements for cert issuance

- A DNS A record `pbx.tendpos.com` (or whatever `PORTAL_HOSTNAME` is) → the
  server's public IP (`5.78.207.2`).
- Ports **80 and 443** reachable on the public IP (`ufw allow 80,443`). Port 80
  is needed for the ACME HTTP challenge / redirect; 443 (TCP + UDP for HTTP/3)
  serves traffic.

## Run / deploy

Runs as the `caddy` service (`caddy:2.7-alpine`) in the compose stack, mounting
`caddy/Caddyfile` read-only with persistent `caddy_data` (certs/ACME account) and
`caddy_config` volumes, depending on `control-plane`. Reload after editing:

```bash
docker compose up -d caddy        # or: docker exec <caddy> caddy reload --config /etc/caddy/Caddyfile
```

Changes ship to the box through the compose deploy (the Caddyfile travels with the
base compose / repo); it is not part of the control-plane image build.

## Gotchas

- The `caddy_data` volume holds the issued cert and ACME account — don't wipe it
  casually or you'll re-hit Let's Encrypt rate limits.
- Auth rate-limiting is intentionally **not** done here (no `caddy-ratelimit`
  plugin); the control-plane already does per-email + per-IP login limits in Go.
- WebSocket upgrades pass through transparently via `reverse_proxy`; no special
  config beyond the `/ws*` handle is needed.
