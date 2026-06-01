# Customer onboarding runbook

The compressed checklist for getting a new customer live on the platform.
Captures every landmine we tripped over during the first onboarding so you
don't have to re-discover them.

Estimated time once familiar: **~10 minutes per customer**.

---

## Pre-flight (one-time, already done in prod)

- [x] `*.pbx.tendpos.com` wildcard A record → `5.78.207.2` (Phase A.1)
- [x] `pbx.tendpos.com` A record → `5.78.207.2` (admin portal)
- [x] CallCentric trunked under main account `17778718016`
- [x] CallCentric IP range `199.87.128.0/19` listed in `trusted-pstn` ACL
- [x] Resend SMTP domain verified (Phase A.7)

---

## Per-customer steps

### 1. Create the tenant in the portal (~30s)

`https://pbx.tendpos.com/admin/` → **+ New tenant**

- Name: `Customer Display Name` (whatever appears on invoices)
- Slug: `<short-id>` (used in URLs and SIP domain — keep lowercase, no spaces)

Once saved:
- A primary `sip_domain` is auto-created as `<slug>.pbx.tendpos.com`
- DNS already resolves it (the wildcard handles all subdomains)

### 2. Invite the customer's admin user (~30s)

Tenant page → **Invitations** → **Invite**:
- Email: their real address
- Role: `tenant_admin`

If SMTP is configured, they get the email. Otherwise the accept-invite URL is
visible inline on the invites page — copy/paste it into Slack/text/whatever.

### 3. Set up the carrier trunk (~3 min)

Tenant page → **Phone trunks** → **Add a trunk** → CallCentric:
- Trunk name: anything for your own reference
- SIP username: `<callcentric_main>` + `<extension>` *(e.g. `17778718016150`)*
- SIP password: the extension's SIP password from CallCentric portal
- Main DID: optional (only set if you also want the trunk's "default" outbound CID)

The platform writes the FreeSWITCH gateway XML and reloads Sofia within ~2 sec.
The status pill on the trunks page goes green (`REGED`) when CallCentric accepts.

### 4. ✋ Configure the CallCentric side (~3 min — landmine territory)

This is where most of the gotchas live. Do **all** of these or inbound calls
will hit CallCentric's voicemail instead of reaching us.

**4a. Extension type must be SIP.**
CallCentric portal → My Account → My Extensions → click the extension you used in step 3.
- **Username** should show as the full `1777...XXX` you registered with
- Look for any "type" / "device" field. It must be set to an **IP/SIP Phone** option.
  NOT "Web Soft Phone", NOT "Voicemail Only".

**4b. Voicemail timer must be ≥ 30 seconds.**
Same extension's **Voicemail** tab (or **Preferences**, varies by CC plan):
- **Voicemail pickup delay** / "Forward to VM after X seconds" → set to **30**.
- The default on a fresh extension is often `0` or `5`, which steals the
  call before SIP delivery completes.
- Or: set Voicemail dropdown to "Use global settings" if the global is sane.

**4c. No competing registrations on the same extension.**
Make sure no other device (mobile app, old desk phone, local docker-compose
running on your laptop) is registered as the same extension. CallCentric
delivers inbound to whichever device registered most recently — if a stale
dev FreeSWITCH at your home IP is competing, calls go nowhere.

Check via the extension's **Edit Extension** page — it shows the registered
device's IP. It should be `5.78.207.2` (our Hetzner box), not your home/cell IP.

**4d. Do Not Disturb off, Call Forwarding off, Call Waiting on.**

**4e. (DIDs only) Forward each DID to the right extension.**
CallCentric portal → DID Forwarding:
- For each DID: select **Extension** → pick the SIP-trunk extension (the
  same one from step 3).
- Save. Confirm by refreshing the page.

### 5. Add DIDs in our platform (~30s per DID — Phase A.2, portal)

Tenant page → **Phone numbers** → **Add a phone number**:

- **Phone number (E.164):** the DID, e.g. `+14155551234` (leading `+` required).
- **On which trunk does this number arrive?** pick the trunk from step 3.
- **Route inbound calls to:** any destination you've created for this tenant —
  an extension, ring group, IVR menu, call queue, or voicemail box. They're
  grouped in the dropdown; create the destination first (tenant overview) if it
  isn't listed yet.
- **CNAM / display name override (optional):** label shown on inbound calls.

The number appears in the routed-numbers table immediately. Use **Simulate
ring** to confirm the dialplan resolves it before placing a real test call, and
**Disable** / **Remove** to take it out of routing.

> Before A.2 this step was a raw `INSERT INTO dids …`. The portal now covers
> all five `destination_kind`s the dialplan routes, so SQL is no longer needed.

### 6. Create extensions for the customer's users (~1 min per ext)

Tenant page → **Extensions** → **Add extension**:
- Extension number (101, 102, ...)
- Display name (Alice, Front Desk, etc.)
- Owner (optional — assign to an invited user later)

The platform generates random SIP credentials. Capture the password from the
extension detail page (the **Show password** button) and hand the username +
password to the user for manual entry into their softphone or desk phone. (A
first-party native softphone app will streamline this later; we're not building
third-party QR/auto-provisioning in the meantime.)

### 7. Provision their device

**Desk phone (recommended — Polycom / Yealink / Grandstream):**
- Get the phone's MAC address.
- Use the ZTP provisioning flow (Phase 5.x — the provisioning HTTPS server
  serves a per-MAC config file).
- Phone boots, fetches its config, registers itself. Done.

**iOS / Android softphone (Linphone):**
- Settings → Accounts → Add account → "Use a third party SIP account"
- Username: `<ext>` (e.g. `101`)
- Password: the SIP password from step 6
- Domain: `<tenant_slug>.pbx.tendpos.com`
- Transport: UDP
- Outbound proxy: leave blank (the wildcard DNS handles it)

> ⚠️ iOS Linphone drops its registration when backgrounded. This is an iOS
> limitation, not a config bug — only a native app with PushKit (months of
> dev work) fixes it. For paying customers we recommend desk phones until
> that's built.

### 8. Test end-to-end

Outbound (softphone → cell):
1. Make the test call from the registered device to your own cell.
2. Should ring within 2-3 seconds.

Inbound (cell → DID):
1. Call the customer's DID from your cell.
2. The customer's phone should ring.

If outbound fails:
- Tail `/var/log/freeswitch/freeswitch.log` on Hetzner for `MANDATORY_IE_MISSING`
  (Kamailio routing issue) or `403 Forbidden` (auth/ACL).
- Check `docker exec sip-platform-kamailio-1 kamcmd ul.dump` — extension
  should appear in the AOR list.

If inbound goes to CallCentric VM:
- Almost always one of the gotchas in step 4 — revisit them.
- Confirm `CallsIN` on the gateway counter increments when you place the
  call: `docker exec sip-platform-freeswitch-1 fs_cli -x "sofia status gateway <gw_name>"`.
- If it stays at 0, the INVITE never reaches us — CallCentric is intercepting.

---

## Troubleshooting cheat sheet

| Symptom | Likely cause | Fix |
|---|---|---|
| 1 ring → CallCentric voicemail | VM timer = 0 / wrong ext type / competing registration | Step 4a + 4b + 4c |
| Inbound never reaches FS (`CallsIN: 0`) | DID forwarding not set, or competing registration outranks us | Step 4e, kill stale dev FS, force gateway re-register |
| Outbound: "the call closes immediately" | Kamailio realm mismatch (auth fails) | Check Linphone's Identity field — domain must match `sip_domain` exactly |
| `MANDATORY_IE_MISSING` in FS log | Kamailio challenged FS for auth (FROM_FREESWITCH route didn't match) | Source IP must be in the dispatcher list or the Docker bridge fallback |
| `403 Forbidden` from internal profile | ACL doesn't include Kamailio's IP | `apply-inbound-acl=local-net` on internal profile |
| Linphone iOS unregisters when backgrounded | iOS background SIP limitation | Use desk phones in prod; native app w/ PushKit is the long-term fix |
| Webphone auth fails after domain rename | Cached ha1 hash from old realm | Revisit `/admin/softphone` to mint fresh creds |

---

## Useful diagnostic commands

```bash
# Who's registered with Kamailio right now?
docker exec sip-platform-kamailio-1 kamcmd ul.dump

# Is the trunk REGED with CallCentric?
docker exec sip-platform-freeswitch-1 fs_cli -x "sofia status gateway <gw_name>"

# Force re-register (kicks any stale registration from old config)
docker exec sip-platform-freeswitch-1 fs_cli -x "sofia profile external killgw <gw_name>"
docker exec sip-platform-freeswitch-1 fs_cli -x "sofia profile external rescan"

# Tail FS log live (it doesn't write to docker stdout — has its own log file)
docker exec sip-platform-freeswitch-1 tail -f /var/log/freeswitch/freeswitch.log

# Simulate the FS dialplan request for a destination
docker exec sip-platform-freeswitch-1 sh -c "wget -q -O- \
  --post-data='section=dialplan&Hunt-Destination-Number=15551234567&Hunt-Context=default&Hunt-Domain=<tenant>.pbx.tendpos.com' \
  http://control-plane:8080/v1/freeswitch/dialplan"

# Daily backup ran?
ls -la /var/backups/postgres/ | tail -5
```

---

## Long-form context

For the *why* behind these landmines — read git history. Specifically:

- `9d7b8a5` — trusted-pstn ACL setup; why we need CallCentric's IP range
- `fix(inbound)` commit — why CallCentric puts the DID in the To: header instead of the Request-URI
- `fix(kamailio)` commit — why FROM_FREESWITCH was silently failing for 24h
- `fix(directory)` commit — the NULL-scan bug that broke softphone outbound for the first day
