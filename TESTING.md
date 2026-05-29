# Local testing

Two paths, depending on what you have installed.

| Path | Validates | Needs |
| --- | --- | --- |
| **A. Control-plane only** | All Go code: admin API, provisioning HTTPS, dialplan handler XML output, E.164 normalization, ring-group dial-string building, ZTP rendering for Polycom/Yealink/Grandstream, schema migrations | Go, Postgres, Redis (all `brew install`-able) |
| **B. Full stack** | Everything above **plus** real SIP REGISTER from softphones, real call routing through Kamailio + FreeSWITCH, audio (with caveats), CallCentric inbound/outbound | Docker or OrbStack, a softphone, optionally a real CallCentric account + public IP for PSTN |

Start with A to shake out anything in our code; do B when you want to see phones actually ring.

---

## Path A — control-plane against local Postgres + Redis (no Docker)

This is the fastest loop and exercises the bulk of what we've written.

### One-shot (recommended)

```bash
brew install go postgresql@16 redis jq    # one-time
bash scripts/dev-up.sh                    # boot the stack + seed
# ... try things (see step 5 below) ...
bash scripts/dev-down.sh                  # tear down
```

`dev-up.sh` is idempotent (safe to re-run), defaults to ports 18080 (admin) + 18443 (provisioning) to avoid colliding with macOS's built-in Apache on 8080. Override with `CP_PORT=` / `PROV_PORT=` env vars if you need different ports.

### Manual steps (if you want to understand each piece)

### 1. Install + start services

```bash
brew install go postgresql@16 redis jq
brew services start postgresql@16
brew services start redis
```

### 2. Create a dev DB and apply migrations

```bash
cd /Volumes/TendPOS/projects/SIP
createdb sipdev
for m in db/migrations/000{1,2,3}_*.up.sql; do
  psql -d sipdev -v ON_ERROR_STOP=1 -f "$m"
done
```

### 3. Build + run the control plane

```bash
cd control-plane && go build -o /tmp/cp ./cmd/server

DATABASE_URL="postgres://$USER@localhost:5432/sipdev?sslmode=disable" \
REDIS_URL="redis://localhost:6379/0" \
CONTROL_PLANE_ADMIN_ADDR=":8080" \
CONTROL_PLANE_PROVISIONING_ADDR=":8443" \
ESL_HOST=127.0.0.1 ESL_PORT=8021 ESL_PASSWORD=ClueCon \
SIP_PUBLIC_HOST=sip.example.local SIP_PUBLIC_PORT=5060 SIP_PUBLIC_TRANSPORT=udp \
PROVISIONING_PUBLIC_HOST=provision.example.local \
/tmp/cp
```

Leave it running. ESL will log warnings about being unable to reach FreeSWITCH — expected and harmless (the goroutine retries with backoff, doesn't block anything).

### 4. Seed test data

In another shell:

```bash
cd /Volumes/TendPOS/projects/SIP
bash scripts/seed.sh
```

Outputs SIP credentials for two extensions plus IDs for ring-group setup.

### 5. Exercise the surface

```bash
# health
curl -s localhost:8080/healthz
curl -s localhost:8443/healthz

# tenants list
curl -s localhost:8080/v1/tenants | jq .

# Simulate a FreeSWITCH dialplan request — internal call ext 101 → 102
curl -s -X POST localhost:8080/v1/freeswitch/dialplan \
  -d 'section=dialplan' -d 'Caller-Context=default' \
  -d 'destination_number=102' \
  -d 'variable_sip_h_X-Sip-Tenant-Domain=acme.sip.local' \
  -d 'Caller-Caller-ID-Number=101'
# → expect <bridge sofia/internal/sip:102@...>

# Outbound PSTN (will return NO_ROUTE_DESTINATION until you add a carrier_account)
curl -s -X POST localhost:8080/v1/freeswitch/dialplan \
  -d 'section=dialplan' -d 'Caller-Context=default' \
  -d 'destination_number=5555551234'

# Create a Yealink device and bind line 1 → ext 101
TID=$(curl -s localhost:8080/v1/tenants | jq -r '.[0].id')
EXT_A_ID=$(psql -d sipdev -tA -c "SELECT id FROM extensions WHERE extension='101'")

curl -s -X POST localhost:8080/v1/tenants/$TID/devices \
  -H 'Content-Type: application/json' \
  -d '{"mac":"00:15:65:ab:cd:ef","vendor":"yealink","model":"t46u"}' | jq .

curl -s -X POST localhost:8080/v1/devices/00:15:65:ab:cd:ef/lines \
  -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg ext "$EXT_A_ID" '{line_number:1, extension_id:$ext}')" | jq .

# Fetch what a phone would download
curl -s localhost:8443/001565abcdef.cfg | head -30
```

Create a ring group and verify the dialplan XML:

```bash
RG=$(curl -s -X POST localhost:8080/v1/tenants/$TID/ring-groups \
  -H 'Content-Type: application/json' \
  -d '{"extension":"200","name":"Sales","strategy":"simultaneous"}')
RGID=$(echo "$RG" | jq -r .id)

EXT_B_ID=$(psql -d sipdev -tA -c "SELECT id FROM extensions WHERE extension='102'")
curl -s -X POST localhost:8080/v1/ring-groups/$RGID/members -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg e $EXT_A_ID '{extension_id:$e}')"
curl -s -X POST localhost:8080/v1/ring-groups/$RGID/members -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg e $EXT_B_ID '{extension_id:$e}')"

# Dial 200 → expect bridge with two legs joined by ","
curl -s -X POST localhost:8080/v1/freeswitch/dialplan \
  -d 'section=dialplan' -d 'Caller-Context=default' \
  -d 'destination_number=200' \
  -d 'variable_sip_h_X-Sip-Tenant-Domain=acme.sip.local'
```

### 6. Run the Go test suite

```bash
cd control-plane && go test ./... -v
```

### 7. Clean up

```bash
pkill -f /tmp/cp
dropdb sipdev
brew services stop redis
# leave postgres running if you use it for other things
```

### What Path A does *not* verify

- That FreeSWITCH and Kamailio start with our configs
- That `mod_xml_curl` actually POSTs the form fields we read (we built against the docs; the contract is small but real)
- That Sofia builds the bridge dialstring the way we wrote it
- That real REGISTER / INVITE flow through Kamailio
- That audio (RTP) flows
- That CallCentric registration succeeds

All of those require Path B.

---

## Path B — full stack with OrbStack (macOS)

**[OrbStack](https://orbstack.dev)** is the right Docker on macOS for this stack. Docker Desktop's networking does not route RTP cleanly; OrbStack gives containers real host networking like Linux does.

### 1. Install OrbStack

```bash
brew install --cask orbstack
open -a OrbStack
# Accept the prompts; it sets up the docker CLI alias
docker version  # should report a server version
```

### 2. Start the stack

```bash
cd /Volumes/TendPOS/projects/SIP
cp .env.example .env

docker compose up -d postgres redis
docker compose --profile tools run --rm migrate up
docker compose up -d control-plane kamailio freeswitch
docker compose ps   # all five "Up" / "healthy"
docker compose logs -f kamailio freeswitch control-plane   # in another shell
```

### 3. Seed + register a softphone

```bash
bash scripts/seed.sh
```

Install a softphone (Linphone, Zoiper 5, MicroSIP on Windows). Use the credentials seed.sh prints. Connect to `127.0.0.1:5060`, realm `acme.sip.local`.

You should see `save() ... 2xx` in Kamailio logs.

### 4. Test calls

| Test | From | Dial | Expect |
| --- | --- | --- | --- |
| Echo | 101 | `9999` | Hear yourself (validates Kamailio → FS → back to phone) |
| Internal | 101 | `102` | Bob's phone rings, audio bridges |
| Ring group | 101 | `200` | Both 101 + 102 ring (simultaneous) or 101 then 102 (sequential) |

### 5. PSTN (optional, needs a real CallCentric account + public IP)

Walk through `scripts/smoke-test.md` section 8.

### Known macOS-specific caveats

- Docker Desktop **will not** give clean audio. Symptom: SIP register works, call connects, no audio (or one-way). Use OrbStack.
- For inbound PSTN you need the FS external port (`5070/udp`) + RTP range (`16384-16484/udp` by default) reachable from the internet. Local-only testing of inbound is not possible.

---

## Path C — full stack on a Linux cloud VM

Often the easiest way to do real telephony testing.

1. Spin up a $6/mo droplet (DigitalOcean) or t3.small (AWS) with Ubuntu 22.04.
2. Install Docker: `curl -fsSL https://get.docker.com | sh`
3. `git clone` (or `rsync`) the repo, then steps 2-4 above.
4. For inbound PSTN: configure CallCentric to register against the droplet's public IP. Open UDP 5070 + 16384-16484 in the cloud firewall.
5. RTP/audio works cleanly because Linux gives real host networking.
