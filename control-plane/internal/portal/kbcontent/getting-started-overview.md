# Welcome: How Your Phone System Is Organized

New to the admin portal? This article gives you the lay of the land so the rest of the help center makes sense.

Your phone system is managed from the web portal at **https://pbx.tendpos.com/admin**. Sign in with the email and password you set when you accepted your invitation.

## The big picture

Your account is a **workspace** (sometimes called a tenant). Everything below lives inside your workspace:

- **Extensions** — the internal "phones." Each person or location gets an extension number (101, 102, …) and a set of SIP credentials their phone uses to sign in.
- **Devices** — the physical desk phones (Yealink, Polycom, Grandstream) that register to extensions.
- **Trunks** — the connection to your phone carrier (CallCentric, Telnyx, etc.) that lets you reach the real phone network.
- **Phone numbers (DIDs)** — the public numbers customers dial, each routed to a destination you choose.
- **Call handling** — ring groups, call queues, IVR menus, and voicemail that decide how inbound calls flow.

## Finding your way around

When you open a workspace you'll see a row of tabs:

1. **Overview** — the home page for your workspace, with a getting-started checklist and quick-create buttons.
2. **Live** — calls happening right now.
3. **Calls** — your call history (CDRs) with search, filters, and CSV export.
4. **Reports** — call volume and summary stats.
5. **Numbers ▾** — Trunks, Phone numbers, Outbound routing, and Business hours.
6. **Messaging ▾** — Messages (SMS), Directory, and Paging / PTT.
7. **Admin ▾** — Team, OIDC SSO, SAML, Webhooks, and the Audit log.

## What to do first

If you're setting up a brand-new workspace, follow the [First-time setup checklist](first-time-setup.md). If you're an everyday user who just wants to manage your own phone, head to [Manage your own phone (self-service)](self-service-portal.md).

## Related

- [First-time setup checklist](first-time-setup.md)
- [Manage your own phone (self-service)](self-service-portal.md)
- [Create and manage extensions](extensions-create-and-manage.md)
