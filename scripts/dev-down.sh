#!/usr/bin/env bash
#
# Tear down what scripts/dev-up.sh started.
# Leaves postgres + redis brew services running (other things might use them).

set -uo pipefail

DBNAME="${DBNAME:-sipdev}"
CP_PIDFILE="/tmp/sip-cp.pid"

if [[ -f "$CP_PIDFILE" ]]; then
  PID=$(cat "$CP_PIDFILE" || true)
  if [[ -n "$PID" ]] && kill -0 "$PID" 2>/dev/null; then
    echo "==> stopping control-plane (pid $PID)"
    kill "$PID" 2>/dev/null || true
  fi
  rm -f "$CP_PIDFILE"
fi

# Catch orphan processes from manual runs.
pkill -f /tmp/sip-cp 2>/dev/null || true

echo "==> dropping database '$DBNAME'"
dropdb "$DBNAME" 2>/dev/null && echo "    dropped" || echo "    (not present)"

rm -f /tmp/sip-cp /tmp/sip-cp.log /tmp/sip-cp.token

echo "==> done. postgres + redis brew services left running."
