# Glossary of phone-system terms

A plain-language reference for the terms you'll see around your phone system.

## People & numbers

**Extension** — An internal phone "line" with a short number (like `1001`).
Each desk phone, softphone, or user maps to an extension.

**DID (Direct Inward Dial)** — A real outside phone number (like
`+1 415 555 0123`) that rings into your system. Also just called a "number."

**Caller ID** — The name and number shown to the person you're calling
(outbound) or that you see when someone calls you (inbound).

**CNAM** — The *name* part of caller ID (e.g. "ACME INC") attached to a number.

**E.164** — The international standard format for phone numbers:
`+` country code + number, no spaces — e.g. `+14155550123`.

## Calling & routing

**Ring group** — A set of extensions that ring together (or in order) when a
number is dialed — e.g. "Sales" rings three phones at once.

**Call queue** — A waiting line for callers when all agents are busy, with
hold music and a strategy for which agent gets the next call.

**IVR (Interactive Voice Response)** — An automated phone menu: "Press 1 for
Sales, 2 for Support."

**Auto attendant** — Another name for an IVR menu that greets and routes
callers.

**Call forwarding** — Sending your calls elsewhere — always, when busy, or
when you don't answer.

**DND (Do Not Disturb)** — Silences your extension; callers go to voicemail or
your forwarding destination.

**Business hours / schedule** — Rules that route calls differently after hours,
on weekends, or on holidays.

**Outbound route** — The rule that decides which carrier/trunk a call leaves
through based on the number dialed.

## Devices & connections

**SIP** — The protocol phones use to set up calls over the internet. Your
"SIP credentials" let a phone or app register to the system.

**Softphone** — A phone app that runs on a computer or mobile device instead of
a physical desk phone. This system has a built-in web softphone.

**Provisioning / ZTP (Zero-Touch Provisioning)** — Automatically configuring a
desk phone by its MAC address so it works out of the box — no manual setup.

**MAC address** — A hardware ID printed on a desk phone, used to provision it.

**Register / registration** — A phone announcing "I'm online and reachable" to
the system. If a phone "won't register," it can't make or take calls.

**Trunk / carrier** — Your connection to the outside phone network (PSTN). The
carrier provides your numbers and routes calls to/from the rest of the world.

**PSTN** — The "Public Switched Telephone Network" — the regular worldwide phone
network your calls reach.

## Features

**Voicemail-to-email** — Voicemails delivered as an email (with the audio
attached) in addition to the mailbox.

**Music on hold (MOH)** — Audio callers hear while on hold or waiting in a
queue.

**Paging group** — Broadcast your voice to many phones at once (overhead
intercom / push-to-talk). See [paging-groups.md](paging-groups.md).

**Broadcast app** — Turn a phone or browser into a paging device — record a
message and blast it, or hold-to-talk live. See
[paging-broadcast-app.md](paging-broadcast-app.md).

## Records & integration

**CDR (Call Detail Record)** — A log entry for each call: who, when, how long,
and the outcome. Your call history is built from CDRs.

**Disposition** — How a call ended: answered, no-answer, busy, etc.

**Webhook** — A way for the system to notify your own software in real time when
something happens (a call completes, a voicemail arrives). See
[integrations-webhooks.md](integrations-webhooks.md).

**API key** — A secret token that lets your software read from or make changes
to your tenant through the REST API. See
[integrations-api-keys.md](integrations-api-keys.md).

## Accounts & security

**Tenant / workspace** — Your isolated organization within the platform. Your
extensions, numbers, and settings are scoped to your tenant.

**Two-factor authentication (2FA)** — A second login step (a code from an app)
on top of your password. See [admin-two-factor.md](admin-two-factor.md).

**SSO (Single Sign-On)** — Logging in through your company identity provider
(OIDC or SAML) instead of a separate password. See [admin-sso.md](admin-sso.md).

**Audit log** — A record of who did what in the admin portal. See
[admin-audit-log.md](admin-audit-log.md).

---

**Related:** [Getting started overview](getting-started-overview.md) ·
[Troubleshooting & FAQ](troubleshooting-faq.md)
