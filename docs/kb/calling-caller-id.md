# Caller ID (what people see when you call)

Control the number and name shown to the people you call, and to whoever calls
your numbers.

## Outbound caller ID (what your callees see)

When someone here dials out, the number presented is decided by the **outbound
route** the call takes:

1. Go to **Outbound routing** for the tenant.
2. On a route, set the **Caller ID number** (an E.164 number you control, e.g.
   `+14155550100`). Calls matching that route present that number.
3. Leave it blank to use the carrier/account default.

Because routes match by dialed prefix, you can present different caller IDs for
different destinations (for example, a local number for local calls). See
[Outbound calling routes](numbers-outbound-routing.md).

> The presented number must be one your carrier permits you to use. Carriers
> reject or override spoofed numbers you don't own.

## Caller ID name (CNAM)

The *name* portion of caller ID (e.g. "ACME INC") is attached to a **phone
number (DID)** as its **CNAM**:

1. Go to **Phone numbers** for the tenant.
2. Set the **CNAM** on the number.

How and whether the name is displayed depends on the destination carrier — many
networks look the name up from their own database rather than the call, so CNAM
behavior isn't guaranteed end to end.

## Inbound caller ID (what you see)

For incoming calls, the caller's number (and any name their carrier sends)
appears on your phone and in your [call history](reports-call-history.md). Ring
groups can also prepend a label (e.g. `[Sales]`) so you know which line is
ringing.

---

**Related:** [Outbound calling routes](numbers-outbound-routing.md) ·
[Phone numbers (DIDs)](numbers-route-dids.md) ·
[Ring groups](calling-ring-groups.md)
