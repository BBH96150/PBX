# Phase 1 smoke test

End-to-end verification: two softphones register against the platform and place an internal call routed Kamailio → FreeSWITCH → Kamailio → callee.

## Prerequisites

- Docker + Docker Compose (on macOS, use [OrbStack](https://orbstack.dev) for working SIP/RTP host networking — Docker Desktop will register the phone but break audio).
- A softphone on the machine running the stack. Tested options:
  - **Linphone** (macOS / Windows / Linux / iOS / Android)
  - **MicroSIP** (Windows)
  - **Zoiper 5** (cross-platform)
  - **Telephone.app** (macOS)
- `curl` and `jq` available locally (`brew install jq`).

## 1. Bring the stack up

```bash
cd /Volumes/TendPOS/projects/SIP
cp .env.example .env       # first run only
docker compose up -d postgres redis
docker compose --profile tools run --rm migrate up
docker compose up -d control-plane kamailio freeswitch
docker compose ps          # all five services should be "Up" / "healthy"
```

Sanity check the control plane:

```bash
curl -fsS http://localhost:8080/healthz       # → {"status":"ok"}
curl -fsS http://localhost:8443/healthz       # → {"status":"ok","service":"provisioning"}
```

Tail logs (in another shell) so you can see what happens during the test:

```bash
docker compose logs -f kamailio freeswitch control-plane
```

## 2. Seed a tenant and two extensions

```bash
bash scripts/seed.sh
```

The script prints SIP credentials for Alice (ext 101) and Bob (ext 102) on the `acme.sip.local` SIP domain.

## 3. Configure softphone A (Alice)

In your softphone, create a new SIP account with:

| Field | Value |
| --- | --- |
| Server / Proxy | `127.0.0.1:5060` |
| SIP Domain / Realm | `acme.sip.local` |
| Username | `101` |
| Auth username | `101` |
| Password | *(printed by seed.sh)* |
| Transport | UDP (TCP also works; TLS in Phase 2) |

In Kamailio logs you should see a successful REGISTER:

```
INFO ... save() => 2xx
```

## 4. Echo-test the media path (Alice → 9999)

Dial **9999** from Alice. FreeSWITCH's static dialplan answers and echoes audio back. You should hear your own voice.

- If you hear yourself: signaling + media path is working.
- If the call connects but no audio: RTP isn't reaching you (Docker NAT issue — see Troubleshooting).
- If you get 404/Busy: control plane returned "not found"; check `docker compose logs control-plane` for the `dialplan lookup` line.

## 5. Configure softphone B (Bob)

Repeat step 3 with Bob's credentials (ext 102).

## 6. Internal call: Alice → Bob

Dial **102** from Alice. Bob's phone should ring; answer to bridge.

Expected log trail:

1. `kamailio`: INVITE arrives from Alice, authenticated, dispatched to `freeswitch:5080`.
2. `freeswitch`: receives INVITE, mod_xml_curl POSTs to control-plane.
3. `control-plane`: `dialplan lookup dest=102 tenant_domain=acme.sip.local`.
4. `freeswitch`: bridges `sofia/internal/sip:102@acme.sip.local;fs_path=sip:kamailio:5060`.
5. `kamailio`: INVITE arrives from FreeSWITCH (matched by `ds_is_from_list`), looks up Bob in usrloc, forwards to Bob.

## Troubleshooting

### No audio (one-way or no audio at all)

This is almost always a Docker networking issue:

- macOS + Docker Desktop: containers can't see your host's RTP source IP. Move to OrbStack (`brew install orbstack`) or run the stack on a Linux VM.
- Linux: switch the Kamailio + FreeSWITCH services to `network_mode: host` in `docker-compose.yml` (and remove their `ports:` blocks).
- Firewall: ensure UDP `${FS_RTP_START}-${FS_RTP_END}` is open.

### `Phase 1 stub - not yet implemented` 404 from Kamailio

The container loaded the placeholder config rather than the real one. Force re-read:

```bash
docker compose restart kamailio
```

### `dialplan lookup` shows up but call fails

Check the bridge URI in control-plane logs. Common issues:
- `tenant_domain` empty → Phase 1 falls back to "any matching extension" (warn). Confirm the softphone is registering with the correct domain.
- `NO_USER_RESPONSE` → the dialed extension doesn't exist in the DB.

### ESL connection refused

Phase 1 the ESL client is a no-op stub (logs "esl client stub"). This is expected and not an error.

## 7. Test zero-touch provisioning

After running `seed.sh` you have a tenant + extensions. Register a device against the same tenant and verify the provisioning server renders a vendor-specific config.

```bash
# Replace UUIDs with the ones seed.sh printed (or fetch via /v1/tenants).
TENANT_ID=$(curl -sf http://localhost:8080/v1/tenants | jq -r '.[0].id')
EXT_ID=$(curl -sf http://localhost:8080/v1/tenants/$TENANT_ID/extensions 2>/dev/null \
  || echo "(extensions list endpoint is Phase 2 — grab the ID from seed.sh output)")

# Create a Yealink T46U device.
curl -sf -X POST http://localhost:8080/v1/tenants/$TENANT_ID/devices \
  -H 'Content-Type: application/json' \
  -d '{"mac":"00:15:65:ab:cd:ef","vendor":"yealink","model":"t46u","label":"Reception desk"}'

# Bind line 1 to extension 101 (use the extension UUID from seed.sh output).
curl -sf -X POST http://localhost:8080/v1/devices/00:15:65:ab:cd:ef/lines \
  -H 'Content-Type: application/json' \
  -d '{"line_number":1,"extension_id":"<EXT_UUID>","label":"101"}'

# Fetch the config the phone would request on boot.
curl -i http://localhost:8443/001565abcdef.cfg
```

You should get back an ini-style Yealink config with `account.1.user_name = 101`, `account.1.password = ...`, `account.1.outbound_proxy.1.address = sip.example.local`, etc.

For other vendors:
- **Polycom**: create device with `"vendor":"polycom"`, fetch `https://provision/0004f2abcdef.cfg`
- **Grandstream**: create device with `"vendor":"grandstream"`, fetch `https://provision/cfgC074AD012345.xml`

In dev (no TLS cert), provisioning runs over plain HTTP on `:8443`. In prod, replace with HTTPS + Let's Encrypt (or carrier-trusted cert for Polycom RPS).

## 8. Phase 2: PSTN via CallCentric

**Pre-reqs**

1. A CallCentric account at https://my.callcentric.com with at least one DID. Note the SIP username (`1777XXXXXXX`-format) and password.
2. Your public IP must be reachable on UDP 5070 (the external SIP profile) and the RTP range `${FS_RTP_START}-${FS_RTP_END}`. For dev that means port-forwarding on your router, or running the stack on a public-IP VM.
3. Run migrations again to apply `0002_phase2_pstn.up.sql`:
   ```bash
   docker compose --profile tools run --rm migrate up
   ```

**Configure FreeSWITCH with your CallCentric credentials**

```bash
cp freeswitch/conf/vars-local.xml.example freeswitch/conf/vars-local.xml
$EDITOR freeswitch/conf/vars-local.xml      # fill in cc_account_username + cc_account_password
docker compose restart freeswitch
```

Verify the gateway registered (in another shell):

```bash
docker compose exec freeswitch fs_cli -x "sofia status gateway callcentric"
# State should be REGED. If FAILED, check creds + ensure CallCentric isn't
# blocking your IP. CallCentric "Lock Account" must be off.
```

**Register the carrier_account + DID in the control plane**

You can do this with the API directly, or use the optional seed script branch:

```bash
CC_USERNAME=17775551234 CC_PASSWORD=yourpass CC_DID=+15555551234 \
  bash scripts/seed.sh
```

That posts a `carrier_account` (linked to the seeded CallCentric carrier) and a DID that routes inbound calls to extension 101 (Alice).

**Outbound test (PSTN call from a softphone)**

From Alice's phone, dial **+15555551212** (or a 10-digit number — the control plane normalizes to E.164). Expected flow:

1. Kamailio: INVITE arrives, authenticated, dispatched to FS internal.
2. FS internal: mod_xml_curl → control plane → `dialplan lookup context=default dest=15555551212`.
3. Control plane: `LooksLikeExternal=true`, normalizes to `+15555551212`, picks primary carrier_account, returns `bridge sofia/gateway/callcentric/15555551212` with `effective_caller_id_number` set to your CallCentric DID.
4. FS external: INVITE → CallCentric → PSTN rings.

If the gateway is unregistered or no carrier_account exists, you'll get `NO_ROUTE_DESTINATION` in the control-plane logs and a fast-busy on the phone.

**Inbound test (PSTN → your DID)**

From any phone, dial your CallCentric DID. Expected flow:

1. CallCentric: INVITE → FS external profile.
2. FS external: mod_xml_curl → control plane → `dialplan lookup context=public dest=15555551234`.
3. Control plane: normalizes to `+15555551234`, looks up in `dids`, finds Alice (ext 101), returns bridge to `sofia/internal/sip:101@acme.sip.local;fs_path=sip:kamailio:5060`.
4. FS internal: INVITE → Kamailio (matched as FROM_FREESWITCH) → usrloc lookup → Alice's phone rings.

If you see `UNALLOCATED_NUMBER` in logs, the DID isn't seeded correctly. Check `curl http://localhost:8080/v1/tenants/$TENANT_ID/dids`.

## Known limitations (Phase 1 + 2)

- No TLS on the SIP side (UDP/TCP only). TLS listener wired in config but disabled by default.
- No NAT traversal beyond what Sofia does natively; phones behind a hard NAT will need rtpengine (Phase 3).
- usrloc is in-memory (`db_mode=0`). Restarting Kamailio drops registrations until phones re-register. Phase 3 moves to db_mode=1 or Redis-backed.
- Single carrier_account is picked for all outbound calls (no tenant-specific outbound routing). Phase 3 adds the `outbound_routes` lookup with prefix matching + per-tenant caller ID overrides.
- DID destination kinds beyond `extension` (ivr, queue, hunt_group, voicemail) return 400 from the admin API and aren't yet wired in the dialplan. They land with the PBX features in Phase 3.
- ZTP works for Polycom, Yealink, Grandstream. Cisco / Snom / Fanvil routes return `501` until templates land.
- True out-of-box ZTP (no factory-reset-and-type-URL) requires manufacturer redirection service integration — Task #10, Phase 3.
- ESL CDR pipeline is live: control plane connects to FS over ESL, listens for `CHANNEL_HANGUP_COMPLETE` (A-leg only), writes one row per call to `cdrs`. After a test call, `SELECT * FROM cdrs ORDER BY started_at DESC LIMIT 5;` should show the call with direction, duration, disposition, and hangup_cause populated.
- The admin API has **no authentication**. Bind it to localhost (default) or put it behind your VPN until Phase 4 adds auth.
- CallCentric carrier credentials live in `freeswitch/conf/vars-local.xml` (gitignored). FS reads them at start-up — `docker compose restart freeswitch` after editing.
