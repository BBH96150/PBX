#!/usr/bin/env bash
#
# Bring up the Path-A dev stack: local Postgres + Redis + control-plane.
# This skips Kamailio / FreeSWITCH — for those, install OrbStack and use
# docker compose (see TESTING.md Path B).
#
# Idempotent: safe to re-run. Tears down a previous instance first.

set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
DBNAME="${DBNAME:-sipdev}"
# Default to high ports so we don't collide with macOS's built-in Apache on :8080.
CP_PORT="${CP_PORT:-18080}"
PROV_PORT="${PROV_PORT:-18443}"
CP_PIDFILE="/tmp/sip-cp.pid"
CP_LOGFILE="/tmp/sip-cp.log"

step() { printf "\n\033[1;36m==> %s\033[0m\n" "$*"; }

need() { command -v "$1" >/dev/null || { echo "missing: $1 (brew install $1)" >&2; exit 1; }; }
need brew
need go
need jq
need curl
need psql

# Stop any previous run.
if [[ -f "$CP_PIDFILE" ]]; then
  PREV=$(cat "$CP_PIDFILE" || true)
  if [[ -n "$PREV" ]] && kill -0 "$PREV" 2>/dev/null; then
    step "stopping previous control-plane (pid $PREV)"
    kill "$PREV" || true
    sleep 1
  fi
  rm -f "$CP_PIDFILE"
fi

step "ensuring postgres + redis are running"
# Accept whatever Postgres flavor brew already has (any of postgresql, postgresql@15, @16, etc.).
PG_PKG=$(brew services list | awk '/^postgresql(@[0-9]+)?[[:space:]]/ {print $1; exit}')
if [[ -z "$PG_PKG" ]]; then
  step "no postgres detected; installing postgresql@16"
  brew install postgresql@16 >/dev/null
  PG_PKG=postgresql@16
fi
# Start it if it isn't already.
if ! brew services list | awk -v pkg="$PG_PKG" '$1 == pkg {print $2}' | grep -q started; then
  brew services start "$PG_PKG" >/dev/null
fi

brew services list | grep -q '^redis' || brew install redis >/dev/null
brew services list | awk '/^redis/ {print $2}' | grep -q started || brew services start redis >/dev/null

# Refuse to clobber a port someone else is using — bail with a clear message.
for port in "$CP_PORT" "$PROV_PORT"; do
  if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
    echo "port $port is already in use:" >&2
    lsof -nP -iTCP:"$port" -sTCP:LISTEN | head -3 >&2
    echo "free it, or re-run with CP_PORT=NNNN / PROV_PORT=NNNN" >&2
    exit 1
  fi
done

# Wait briefly for postgres to accept connections.
for i in {1..15}; do
  pg_isready -h 127.0.0.1 -p 5432 -q && break
  sleep 1
done

step "(re)creating database '$DBNAME'"
dropdb "$DBNAME" 2>/dev/null || true
createdb "$DBNAME"

step "applying migrations"
for m in "$REPO"/db/migrations/0*_*.up.sql; do
  psql -d "$DBNAME" -v ON_ERROR_STOP=1 -f "$m" >/dev/null
  echo "    applied $(basename "$m")"
done

step "building control-plane"
(cd "$REPO/control-plane" && go build -o /tmp/sip-cp ./cmd/server)

# Phase 4.0: generate a bootstrap admin token so the dev stack starts with
# auth wired up. The script writes the token to /tmp/sip-cp.token and
# exports it as API_TOKEN for the seed script.
BOOTSTRAP_TOKEN="sip_$(openssl rand -hex 24 2>/dev/null || python3 -c 'import secrets;print(secrets.token_hex(24))')"
echo "$BOOTSTRAP_TOKEN" > /tmp/sip-cp.token
chmod 600 /tmp/sip-cp.token
export API_TOKEN="$BOOTSTRAP_TOKEN"

step "starting control-plane on :$CP_PORT (admin) + :$PROV_PORT (provisioning)"
DATABASE_URL="postgres://$USER@localhost:5432/$DBNAME?sslmode=disable" \
REDIS_URL="redis://localhost:6379/0" \
CONTROL_PLANE_ADMIN_ADDR=":$CP_PORT" \
CONTROL_PLANE_PROVISIONING_ADDR=":$PROV_PORT" \
ESL_HOST=127.0.0.1 ESL_PORT=8021 ESL_PASSWORD=ClueCon \
SIP_PUBLIC_HOST=sip.example.local SIP_PUBLIC_PORT=5060 SIP_PUBLIC_TRANSPORT=udp \
PROVISIONING_PUBLIC_HOST=provision.example.local \
KAMAILIO_SIP_TARGET=kamailio:5060 \
BOOTSTRAP_API_TOKEN="$BOOTSTRAP_TOKEN" \
nohup /tmp/sip-cp >"$CP_LOGFILE" 2>&1 &
echo $! > "$CP_PIDFILE"
disown || true

# Wait for /healthz.
for i in {1..20}; do
  curl -sf "http://localhost:$CP_PORT/healthz" >/dev/null && break
  sleep 0.5
done
curl -sf "http://localhost:$CP_PORT/healthz" >/dev/null || {
  echo "control-plane failed to come up — last log lines:" >&2
  tail -20 "$CP_LOGFILE" >&2
  exit 1
}

step "seeding tenant + 2 extensions"
API_HOST="http://localhost:$CP_PORT" bash "$REPO/scripts/seed.sh"

cat <<EOF

==> ready

  admin API       : http://localhost:$CP_PORT
  provisioning    : http://localhost:$PROV_PORT
  control-plane pid: $(cat "$CP_PIDFILE")
  log             : $CP_LOGFILE
  database        : $DBNAME (psql -d $DBNAME)

  API token (super-admin, dev only):
    $BOOTSTRAP_TOKEN
  also saved to: /tmp/sip-cp.token

Try:
  export API_TOKEN=\$(cat /tmp/sip-cp.token)
  curl -s -H "Authorization: Bearer \$API_TOKEN" http://localhost:$CP_PORT/v1/tenants | jq .

Stop everything: bash scripts/dev-down.sh
EOF
