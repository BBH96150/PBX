# Emergency calling (911) — important

> ⚠️ **Read this before relying on the system for emergency calls.** Internet
> phone service handles emergency calls differently from a traditional landline.
> Make sure everyone who uses a phone here understands the limits below.

## How emergency calls work here

Emergency calls (911 in North America) leave through your **carrier/SIP trunk**,
the same path as any other outside call. The platform itself does not register
your physical location — **your carrier does**. So getting 911 right is mostly
about how your carrier account and addresses are set up.

## What you must do

1. **Register a service address with your carrier** for each number/line that
   could place a 911 call. With CallCentric (and most VoIP carriers) this is
   done in the carrier's portal, not here. The address you register is what the
   911 dispatcher (PSAP) sees.
2. **Keep addresses current.** If a phone, softphone, or user moves to a new
   location, update the registered address with the carrier. The system can't
   know where a softphone physically is.
3. **Confirm an outbound route exists** so `911` actually reaches the carrier —
   see [outbound routing](numbers-outbound-routing.md). Don't block or rewrite
   emergency numbers.
4. **Tell your users** about the limitations below.

## Limitations to understand

- **Power/internet outage:** if the phone, the internet, or the platform is
  down, emergency calls won't go through. Keep an alternate means (e.g. a mobile
  phone) available.
- **Nomadic use:** a softphone or web phone can be used from anywhere, but 911
  will route based on the **registered address**, which may not be where you
  actually are. Always be ready to state your real location to the dispatcher.
- **Shared numbers / SIP trunks:** the dispatched address is tied to the number
  the call goes out on — make sure that mapping is correct at the carrier.

## Test responsibly

Do **not** dial 911 to test. If you want to verify routing, ask your carrier
whether they provide a 933 (or similar) address-verification test number, and
use that.

---

**Related:** [Connect a carrier / SIP trunk](trunks-connect-carrier.md) ·
[Outbound routing](numbers-outbound-routing.md) ·
[Phone numbers (DIDs)](numbers-route-dids.md)
