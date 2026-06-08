# Architecture & call flows

This is the developer-level "how it actually works" companion to the top-level
[`README.md`](../README.md). It explains the moving parts and walks through the
main call flows end to end.

## The pieces

| Component | Role |
|---|---|
| **Kamailio** | SIP edge: registrar + authentication + proxy. Phones/softphones REGISTER and send all SIP here. Holds the location table (who's reachable where). Front door for SIP-over-WebSocket. |
| **FreeSWITCH** | Media server + PBX engine: bridging, conferences, IVR, queues, voicemail, recording, PSTN gateways. Has **no static dialplan** — it asks the control-plane per call. |
| **Control-plane (Go)** | The brain. Serves FreeSWITCH's dialplan/directory/config over `xml_curl`; exposes the `/v1` REST API + admin portal; runs ZTP provisioning; ingests CDRs and fires webhooks. |
| **Postgres** | System of record (tenants, extensions, DIDs, call-handling, CDRs, auth) — and Kamailio's `location`/`subscriber` tables. |
| **Redis** | Ephemeral state / rate-limit counters. |
| **rtpengine** | WebRTC media bridge (DTLS-SRTP/ICE ↔ plain RTP), driven by Kamailio for WS/WSS legs only. |
| **Caddy** | Public TLS front door: terminates HTTPS, reverse-proxies the portal/API, and proxies `/ws` to Kamailio for browser SIP. |

The defining idea: **FreeSWITCH is dumb; the control-plane is smart.** Every
routing decision is a live HTTP call from FS to the Go service, which reads
Postgres and returns the exact XML to execute. That's what makes the system
multi-tenant and reconfigurable without restarting FreeSWITCH.

## Registration

```
Phone ──REGISTER──▶ Kamailio ──auth_db (subscriber, HA1)──▶ Postgres
                       │  on success: save() ──▶ location table (usrloc, db_mode)
                       ▼
                    200 OK
```

Desk phones register over UDP/TCP; browsers register over WebSocket (Caddy
terminates TLS at `/ws` and proxies to Kamailio's WS listener). Auth is digest
against the `subscriber` table (HA1 computed at provisioning time). Registrations
live in the Postgres `location` table, so the portal's live/presence views can
read them directly.

## Internal call (extension → extension)

```
Phone A ──INVITE 1002──▶ Kamailio ──(auth, append tenant headers)──▶ FreeSWITCH
                                                                         │
                              xml_curl: GET /v1/freeswitch/dialplan ◀────┘
                                   (control-plane looks up 1002 in this tenant)
                                                                         │
FreeSWITCH ──INVITE──▶ Kamailio ──location lookup──▶ Phone B ◀───────────┘
            (media bridged through FreeSWITCH)
```

## Inbound PSTN (a DID rings in)

```
Carrier ──INVITE +1415…──▶ FreeSWITCH (external SOFIA profile)
                              │ xml_curl dialplan: who owns this DID?
                              ▼
                 control-plane resolves the DID → destination:
                   extension | ring group | IVR | queue | voicemail
                              │
                 (business-hours schedule may pick a different destination)
                              ▼
              FreeSWITCH executes it, bridging via Kamailio to the phone(s)
```

## Outbound PSTN (a phone dials out)

```
Phone ──INVITE +1818…──▶ Kamailio ──(auth)──▶ FreeSWITCH
                                                  │ xml_curl dialplan
                                                  ▼
                       control-plane picks the tenant's outbound route →
                       carrier account → the matching SOFIA gateway
                                                  ▼
                       FreeSWITCH ──INVITE──▶ Carrier gateway ──▶ PSTN
```

Per-tenant carrier gateways are written as XML into a shared volume by the
control-plane and picked up on a SOFIA rescan (dynamic provisioning).

## Paging (one-to-many)

Dialing a paging group's page code routes (after extensions/ring-groups/IVRs/
queues miss) to the paging handler:

```
Phone ──INVITE 800──▶ Kamailio ──▶ FreeSWITCH ──xml_curl──▶ control-plane
   control-plane returns a dialplan that:
     answer → "go ahead" tone →
     conference_set_auto_outcall(every member, muted, sip_auto_answer) →
     conference paging_<group>@paging  (the pager is the unmuted moderator)
```

Members auto-answer hands-free and listen; the pager talks. The `paging`
conference profile is a static file so mod_conference loads it at boot even
before the control-plane is reachable. See
[kb/paging-groups.md](kb/paging-groups.md) and the
[broadcast app](kb/paging-broadcast-app.md).

## Browser calling / live PTT (WebRTC)

```
Browser ──wss──▶ Caddy (/ws, TLS) ──ws──▶ Kamailio
   Kamailio (WS/WSS leg): rtpengine_offer → plain RTP toward FreeSWITCH
   Kamailio (reply):      rtpengine_answer → DTLS-SRTP/ICE back to the browser
                                   │
Browser ⇄ rtpengine ⇄ FreeSWITCH  (media)
```

rtpengine bridges the browser's WebRTC media to plain RTP for FreeSWITCH. It is
only invoked for WS/WSS legs — desk-phone media never touches it.

## CDR + webhook pipeline

```
FreeSWITCH ──ESL events (CHANNEL_HANGUP_COMPLETE)──▶ control-plane
   → normalize → insert into cdrs (with note, recording path, billable sec)
   → fire `call.completed` webhook to subscribed tenant endpoints (HMAC-signed)
```

Trunk state changes (`trunk.down`/`trunk.up`) and new voicemails
(`voicemail.new`) fire webhooks the same way. See
[kb/integrations-webhooks.md](kb/integrations-webhooks.md).

## Deploy model (why some changes need ops workflows)

CI builds the control-plane image and, on green, recreates **only the
control-plane container** — so anything served via `xml_curl` (dialplan,
directory, IVR/queue/conference config) ships with a normal push. Kamailio,
FreeSWITCH, and Caddy **configs are not synced by CI**; they're applied through
the `ops-*` workflows (which also restart the relevant service and inject
secrets the repo files don't carry). See [`CONTRIBUTING.md`](../CONTRIBUTING.md).
