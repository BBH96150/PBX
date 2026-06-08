# Outbound Calling Routes

Outbound routes decide which trunk carries a call when someone in your workspace dials an outside number — and let you control the caller ID presented per destination.

## Do you even need this?

With no rules configured, outbound calls automatically use your default trunk. You only need routes here if you want to:

- Split traffic across multiple trunks (for example, send international calls via a different carrier), or
- Present a different caller ID depending on the number dialed.

## How matching works

Each rule has a **match prefix** in E.164 form. The dialed number is matched against every enabled rule, and the **most specific** prefix wins — `+1415` beats `+1`, which beats a blank catch-all. If two rules share the same prefix length, the lower **Priority** number wins.

## Add a route

1. Go to **Numbers ▾ → Outbound routing**.
2. In **Add a route**, fill in:
   - **Route name** — for example "US domestic".
   - **Match prefix (E.164)** — for example `+1`. Leave blank for a catch-all.
   - **Carry the call on** — the trunk this route uses.
   - **Caller ID override** (optional) — the number to present; blank uses the trunk's main DID.
   - **Priority** — tie-breaker (lower wins).
3. Click **Add route**.

## Enable, disable, or remove a route

Use **Disable** / **Enable** to toggle a rule, and **Remove** to delete it — matching calls then fall through to the next rule or your default trunk.

## Related

- [Connect a carrier (SIP trunk)](trunks-connect-carrier.md)
- [Route inbound phone numbers (DIDs)](numbers-route-dids.md)
