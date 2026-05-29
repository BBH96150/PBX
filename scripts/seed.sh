#!/usr/bin/env bash
#
# Seed one tenant + one SIP domain + two extensions via the control-plane
# admin API. Prints SIP credentials for both extensions so you can paste
# them into a softphone.
#
# Usage:
#   bash scripts/seed.sh                            # uses defaults below
#   API_HOST=http://localhost:8080 bash scripts/seed.sh

set -euo pipefail

API="${API_HOST:-http://localhost:8080}"
TENANT_SLUG="${TENANT_SLUG:-acme}"
TENANT_NAME="${TENANT_NAME:-Acme Corp}"
SIP_DOMAIN="${SIP_DOMAIN:-acme.sip.local}"
EXT_A="${EXT_A:-101}"
EXT_B="${EXT_B:-102}"

need() { command -v "$1" >/dev/null || { echo "missing: $1" >&2; exit 1; }; }
need curl
need jq

echo "==> creating tenant ${TENANT_SLUG} at ${API}"
TENANT=$(curl -sf -X POST -H "Authorization: Bearer ${API_TOKEN}" "${API}/v1/tenants" \
  -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg s "$TENANT_SLUG" --arg n "$TENANT_NAME" '{slug:$s,name:$n}')")
TENANT_ID=$(echo "$TENANT" | jq -r .id)
echo "    tenant_id: ${TENANT_ID}"

echo "==> creating SIP domain ${SIP_DOMAIN}"
DOMAIN=$(curl -sf -X POST -H "Authorization: Bearer ${API_TOKEN}" "${API}/v1/tenants/${TENANT_ID}/sip-domains" \
  -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg d "$SIP_DOMAIN" '{domain:$d, is_primary:true}')")
DOMAIN_ID=$(echo "$DOMAIN" | jq -r .id)
echo "    sip_domain_id: ${DOMAIN_ID}"

create_extension () {
  local ext="$1"
  local display="$2"
  curl -sf -X POST -H "Authorization: Bearer ${API_TOKEN}" "${API}/v1/tenants/${TENANT_ID}/extensions" \
    -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg d "$DOMAIN_ID" --arg e "$ext" --arg dn "$display" \
        '{sip_domain_id:$d, extension:$e, display_name:$dn}')"
}

echo "==> creating extension ${EXT_A}"
A=$(create_extension "$EXT_A" "Alice")
echo "==> creating extension ${EXT_B}"
B=$(create_extension "$EXT_B" "Bob")

cat <<EOF

============================================================
Softphone credentials
============================================================

Server (SIP proxy):  127.0.0.1:5060   (UDP or TCP)
Realm / SIP domain:  ${SIP_DOMAIN}
Transport:           UDP (TCP also works; TLS in Phase 2)

Extension A (Alice)
  ext      : ${EXT_A}
  username : $(echo "$A" | jq -r .sip_username)
  password : $(echo "$A" | jq -r .sip_password)
  id       : $(echo "$A" | jq -r .id)

Extension B (Bob)
  ext      : ${EXT_B}
  username : $(echo "$B" | jq -r .sip_username)
  password : $(echo "$B" | jq -r .sip_password)
  id       : $(echo "$B" | jq -r .id)

For ZTP testing (Task #8/#9):
  tenant_id     = ${TENANT_ID}
  sip_domain_id = ${DOMAIN_ID}
  extension_id  = (use one of the IDs above when binding device lines)

Quick tests:
  Dial 9999 → echo test (audio loopback via FreeSWITCH)
  Dial ${EXT_B} from ${EXT_A} → internal call routed via FS
============================================================
EOF

# ---------------------------------------------------------------------------
# Optional Phase 2 seed: CallCentric account + DID
# Triggered when CC_USERNAME, CC_PASSWORD, and CC_DID are all set.
# ---------------------------------------------------------------------------
if [[ -n "${CC_USERNAME:-}" && -n "${CC_PASSWORD:-}" && -n "${CC_DID:-}" ]]; then
  echo
  echo "==> seeding CallCentric carrier_account + DID (Phase 2)"

  ACCT=$(curl -sf -X POST -H "Authorization: Bearer ${API_TOKEN}" "${API}/v1/carriers/callcentric/accounts" \
    -H 'Content-Type: application/json' \
    -d "$(jq -nc \
        --arg user "$CC_USERNAME" \
        --arg pass "$CC_PASSWORD" \
        --arg did "$CC_DID" \
        '{name:("CallCentric " + $user),
          sip_username:$user, sip_password:$pass,
          fs_gateway_name:"callcentric",
          register:true,
          main_did_e164:$did}')")
  ACCT_ID=$(echo "$ACCT" | jq -r .id)
  echo "    carrier_account_id: ${ACCT_ID}"

  CARRIER_ID=$(curl -sf -H "Authorization: Bearer ${API_TOKEN}" "${API}/v1/carriers" | jq -r '.[] | select(.kind=="callcentric") | .id')

  # Route inbound DID to extension A by default.
  EXT_A_ID=$(echo "$A" | jq -r .id)
  DID=$(curl -sf -X POST -H "Authorization: Bearer ${API_TOKEN}" "${API}/v1/tenants/${TENANT_ID}/dids" \
    -H 'Content-Type: application/json' \
    -d "$(jq -nc \
        --arg carrier "$CARRIER_ID" \
        --arg acct "$ACCT_ID" \
        --arg e164 "$CC_DID" \
        --arg ext  "$EXT_A_ID" \
        '{carrier_id:$carrier, carrier_account_id:$acct,
          e164:$e164, destination_kind:"extension", destination_id:$ext}')")
  echo "    DID: $(echo "$DID" | jq -r .e164) → ext ${EXT_A}"
  echo
  echo "    NOTE: also update freeswitch/conf/vars-local.xml with the same"
  echo "    CallCentric username/password and restart FreeSWITCH so the"
  echo "    sofia gateway can register."
fi

echo
echo "==> done"
