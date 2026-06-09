# Porting an existing phone number

Want to keep a number you already have (from another phone company) and use it
here? That's called **porting**. Porting happens at the **carrier** level, then
you point the number at this platform.

## How it works

1. **Don't cancel your old service yet.** The number must stay active at your
   current provider until the port completes — cancelling early can cause the
   number to be lost.
2. **Start the port with your carrier** (the SIP trunk provider you use here,
   e.g. CallCentric). You'll typically provide:
   - The number(s) to port.
   - The current provider's account number and a recent bill.
   - The service address and authorized contact on the existing account.
   - A signed Letter of Authorization (LOA), if requested.
3. **Wait for the port date.** Simple ports often take a few business days;
   complex ones longer. Your carrier confirms a firm date.
4. **Add the number here** once it's live on your carrier: go to
   **Phone numbers (DIDs)**, add the number, and route it to an extension, ring
   group, IVR, or queue. See [Phone numbers (DIDs)](numbers-route-dids.md).
5. **Test, then cancel the old service.** Place a test call to the number,
   confirm it rings the right destination, and only then close the old account.

## Tips

- Names/addresses on the port request must **match the old account exactly** —
  mismatches are the most common cause of delays.
- Toll-free numbers and some number types port differently — ask your carrier.
- Re-register the ported number's **emergency address** with your carrier — see
  [Emergency calling (911)](emergency-calling.md).

---

**Related:** [Connect a carrier / SIP trunk](trunks-connect-carrier.md) ·
[Phone numbers (DIDs)](numbers-route-dids.md) ·
[Emergency calling](emergency-calling.md)
