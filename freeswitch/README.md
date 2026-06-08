# freeswitch

The media server and PBX engine. It handles all RTP/SRTP media and every
call-handling feature — voicemail, IVR, ring/hunt groups, call-center queues,
conferences (used for paging), and call recording. Kamailio dispatches
authenticated calls to it; PSTN reaches it through Sofia external-profile
gateways.

## The key idea: no static dialplan

FreeSWITCH here is driven almost entirely by the Go control-plane over
`mod_xml_curl`. Per-call, FreeSWITCH asks the control-plane for:

- **dialplan** — `http://control-plane:8080/v1/freeswitch/dialplan`
- **directory** — `http://control-plane:8080/v1/freeswitch/directory`
  (used by `mod_voicemail` for per-user vm settings; SIP auth itself is Kamailio's
  job)
- **configuration** — `http://control-plane:8080/v1/freeswitch/configuration`
  (e.g. runtime IVR config)

So routing/voicemail/IVR behavior lives in the control-plane and Postgres, not in
the XML files in this directory. The XML here is mostly bootstrap, module
loading, and Sofia profile setup.

## Key files

| Path | Purpose |
| --- | --- |
| `conf/freeswitch.xml` | Top-level include tree. |
| `conf/vars.xml`, `conf/vars-local.xml(.example)` | Global vars; `vars-local.xml` carries the public IP / RTP advertise settings (falls back to `stun:stun.freeswitch.org` in dev). |
| `conf/autoload_configs/modules.conf.xml` | Module load list (sofia, xml_curl, event_socket, voicemail, callcenter, conference, …). |
| `conf/autoload_configs/xml_curl.conf.xml` | Binds dialplan/directory/configuration lookups to the control-plane URLs (2s timeout). |
| `conf/autoload_configs/conference.conf.xml` | Conference profiles — used by the paging / PTT subsystem. |
| `conf/autoload_configs/sofia.conf.xml`, `event_socket.conf.xml`, `acl.conf.xml`, `voicemail.conf.xml`, `callcenter.conf.xml`, `switch.conf.xml`, `logfile.conf.xml`, `local_stream.conf.xml` | Core/module configs. |
| `conf/sip_profiles/internal.xml` | Internal Sofia profile (faces Kamailio). |
| `conf/sip_profiles/external.xml` | External Sofia profile (faces PSTN carriers); includes `external/dynamic/*.xml`. |
| `conf/sip_profiles/external/dynamic/` | **Per-tenant carrier gateway XML written at runtime by the control-plane**, picked up on `sofia rescan`. |
| `conf/dialplan/default.xml`, `features.xml` | Minimal dialplan stubs (real routing comes from `xml_curl`). |

## Run / test locally

Started by the root compose as the `freeswitch` service
(`safarov/freeswitch:latest`), mounting `freeswitch/conf` read-only plus volumes
for the dynamic gateway dir, recordings, db, and logs.

```bash
docker compose up -d freeswitch
docker exec sip-platform-freeswitch-1 fs_cli -x "sofia status"
docker exec sip-platform-freeswitch-1 fs_cli -x "sofia status gateway <gw_name>"
docker exec sip-platform-freeswitch-1 tail -f /var/log/freeswitch/freeswitch.log
# Simulate the control-plane dialplan request:
docker exec sip-platform-freeswitch-1 sh -c "wget -q -O- \
  --post-data='section=dialplan&Hunt-Destination-Number=15551234567&Hunt-Context=default&Hunt-Domain=<tenant>.pbx.tendpos.com' \
  http://control-plane:8080/v1/freeswitch/dialplan"
```

> FreeSWITCH writes to its own log file (`/var/log/freeswitch/freeswitch.log`),
> not Docker stdout — `tail` the file, don't `docker logs`.

## Configuration (env vars passed by compose)

| Var | Purpose |
| --- | --- |
| `FS_RTP_START` / `FS_RTP_END` | RTP port range. |
| `EXT_SIP_IP` / `EXT_RTP_IP` | Public IP advertised in outbound SDP (prod: server's static IP; dev: unset → STUN auto-discovery). |
| ESL password | Event socket auth (control-plane connects on `:8021`). |

The prod compose override adds a `freeswitch_storage` mount (shared with the
control-plane for voicemail/recording playback and paging blasts) and replaces
the image's ESL-authenticating healthcheck with a password-free port-listening
probe.

## Deploy

> **CRITICAL:** `ci.yml` does **NOT** sync `freeswitch/conf` to the box. Editing
> files here does not change production by itself. Apply config to the box via:
> - `restart-sip.yml` (optionally syncs conf dirs — destructive, overwrites
>   server-side edits — then restarts), or
> - `ops-paging.yml` / `ops-webrtc.yml` / `ops-fs-diag.yml` for the targeted
>   paging / WebRTC media / queue-diagnostics runbooks.

Per-tenant carrier gateways are not hand-edited: the control-plane writes them to
`sip_profiles/external/dynamic/` and triggers a Sofia rescan over ESL when a
trunk is added in the portal.

## Gotchas

- **Inbound goes to carrier voicemail / `CallsIN: 0`** — almost always a
  carrier-side misconfig (extension type, VM timer, competing registration, DID
  forwarding). See [`docs/onboarding.md`](../docs/onboarding.md) step 4.
- **`403 Forbidden` on the internal profile** — the ACL must include Kamailio's
  IP (`apply-inbound-acl=local-net`).
- **Paging** depends on `mod_conference` + `conference.conf.xml` being loaded;
  validate with `ops-paging.yml`.
