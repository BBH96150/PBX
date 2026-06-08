# First-Time Setup Checklist

This is the fast path to getting your workspace making and receiving real phone calls. Plan about 15 minutes. Each step links to a detailed article if you want more.

## Before you start

Make sure you can sign in to the admin portal at **https://pbx.tendpos.com/admin** and that you can see your workspace's **Overview** page.

## The steps

1. **Connect a carrier trunk.** Go to **Numbers ▾ → Trunks** and add the SIP trunk from your carrier (for example, CallCentric). Wait for the live registration pill to turn green (**REGED**). See [Connect a carrier (SIP trunk)](trunks-connect-carrier.md).
2. **Create your extensions.** On the **Overview** page, add an extension for each person or location. You can add them one at a time or paste a whole list. See [Create and manage extensions](extensions-create-and-manage.md).
3. **Set up your phones.** Either provision a desk phone by its MAC address (zero-touch) or hand each user their SIP credentials for a softphone. See [Add and provision desk phones](devices-provision-desk-phones.md).
4. **Decide where inbound calls go.** Create any ring groups, IVR menus, queues, or voicemail boxes you need first, so they're available as routing destinations. See [Ring groups](calling-ring-groups.md), [IVR menus](calling-ivr-menus.md), [Call queues](calling-call-queues.md).
5. **Route your phone numbers.** Go to **Numbers ▾ → Phone numbers** and point each number at the extension or call-handling destination you want. Use **Simulate ring** to test before going live. See [Route inbound phone numbers (DIDs)](numbers-route-dids.md).
6. **Run the setup check.** Open your workspace setup check (the **Setup check** page) to confirm everything is wired up. It flags anything still missing as **CHECK** or **FAIL**, with a quick link to fix it.

## Tips

- Create destinations (ring groups, IVRs, queues, voicemail) **before** routing numbers — they only appear in the routing dropdown once they exist.
- The trunk **Test call** button and the phone-number **Simulate ring** button let you verify each piece independently before placing a real call.

## Related

- [Connect a carrier (SIP trunk)](trunks-connect-carrier.md)
- [Create and manage extensions](extensions-create-and-manage.md)
- [Route inbound phone numbers (DIDs)](numbers-route-dids.md)
- [Troubleshooting & FAQ](troubleshooting-faq.md)
