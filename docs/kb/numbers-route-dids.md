# Route Inbound Phone Numbers (DIDs)

A DID (Direct Inward Dialing number) is a phone number your carrier assigns to you. This page decides where each inbound call lands — an extension, ring group, IVR menu, call queue, or voicemail box.

## Before you start

- You need at least one [trunk](trunks-connect-carrier.md) configured.
- You need at least one destination to route to. Create your extensions, ring groups, IVRs, queues, or voicemail boxes first — they only appear in the routing dropdown once they exist.

## Add a phone number

1. Go to **Numbers ▾ → Phone numbers**.
2. In **Add a phone number**, fill in:
   - **Phone number (E.164)** — include the leading `+` and country code, for example `+14155551234`.
   - **On which trunk does this number arrive?** — pick the trunk the carrier delivers it on.
   - **Route inbound calls to** — choose the destination. Options are grouped by type (extensions, ring groups, IVRs, queues, voicemail).
   - **CNAM / display name override** (optional) — a label shown on inbound calls, handy for telling which line rang.
3. Click **Add number**.

## Test routing before going live

Click **Simulate ring** on a number's row to confirm the system resolves it to the right destination — no real call needed.

## Change where a number goes

1. Expand **Edit routing** on the number's row.
2. Pick a new destination and/or update the CNAM override.
3. Click **Save routing**.

## After-hours routing

If you've created a [business-hours schedule](numbers-business-hours.md), expand **After-hours routing** on a number's row to:

1. Choose the **Schedule** to apply.
2. Set **When closed, route to** — typically a voicemail box or a different greeting.
3. Click **Save after-hours**.

Calls then route to the normal destination during open hours and to the closed destination outside them.

## Enable, disable, or remove a number

Use **Disable** to temporarily stop a number from routing, **Enable** to bring it back, and **Remove** to take it out of routing entirely (inbound calls to it will fail until reassigned).

## Related

- [Connect a carrier (SIP trunk)](trunks-connect-carrier.md)
- [Business hours and after-hours routing](numbers-business-hours.md)
- [Ring groups](calling-ring-groups.md), [IVR menus](calling-ivr-menus.md), [Call queues](calling-call-queues.md)
