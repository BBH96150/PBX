# Connect a Carrier (SIP Trunk)

A SIP trunk connects your phone system to the public phone network (PSTN) through a carrier such as **CallCentric**, **Telnyx**, or **Bandwidth**. Once a trunk is registered, your extensions can dial real numbers and receive calls on your DIDs.

## Add a trunk

1. Go to **Numbers ▾ → Trunks**.
2. In **Add a trunk**, fill in:
   - **Carrier** — pick your provider from the list.
   - **Trunk name** — anything for your own reference (for example "Main CallCentric line").
   - **SIP username** — from your carrier.
   - **SIP password** — from your carrier.
   - **Main DID** — the phone number the carrier routes to you, in E.164 format (for example `+14155551234`). This is used for caller ID on outbound calls.
3. Click **Save trunk**. The platform configures the connection and starts registering within a few seconds.

### CallCentric username format

- **Main account:** 11 digits starting with `1777` (for example `17771234567`).
- **Sub-account / extension:** main account + `*` + sub-number (for example `17771234567*100`). The asterisk is required.
- Do **not** use your DID or your CallCentric login email — those won't work.
- Find the right value and the SIP password in the CallCentric portal under **My Account → Account Info** (the SIP password is separate from your web login).

## Confirm registration

The **Live registration** column polls your carrier every couple of seconds:

- **REGED** — registered and ready to make/receive calls.
- **TRYING** — mid-registration; should flip to REGED within a few seconds.
- **FAIL_WAIT** — credentials or network are wrong; recheck your carrier portal.

## Place a test call

1. Expand **Test call** on the trunk row.
2. Enter a destination (your cell, or any number in E.164 format).
3. Click **Place test call**. When you answer, speak — you'll hear yourself echoed back with about a one-second delay, confirming two-way audio.

## Advanced options

If your carrier assigns you a specific server or you hit a "403 Incorrect Authentication" with correct credentials, expand **Advanced** to set a **Proxy host override**, **Proxy port override**, **Auth realm**, or **Transport** (UDP/TCP/TLS). For IP-authenticated trunks that don't register, uncheck **Register with carrier**.

## Trunk down alerts

Set a **Trunk alert recipient** at the top of the Trunks page to control where trunk down/recovery emails go. Leave it blank to email your workspace admins automatically. See [Trunk-down alerts and daily digest](integrations-alerts-and-digest.md).

## Related

- [Route inbound phone numbers (DIDs)](numbers-route-dids.md)
- [Outbound calling routes](numbers-outbound-routing.md)
- [Troubleshooting & FAQ](troubleshooting-faq.md)
