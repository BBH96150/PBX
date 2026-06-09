# Paging Groups (Intercom / PTT)

Paging groups let one person talk to many at once — an overhead intercom announcement or a push-to-talk channel. Find them under **Messaging ▾ → Paging / PTT**.

## Delivery modes

Each group has a delivery mode:

- **Conference page** — dial the group's page code and members auto-answer into a one-way page. Works on any registered phone or softphone.
- **Multicast** — desk phones listen on a multicast address pushed via provisioning. LAN only, lowest latency. *Multicast listening is auto-provisioned on Yealink phones today; for Polycom/Grandstream, use Conference page (which works on every phone) or configure multicast on the phone manually.*
- **Native PTT** — a hold-to-talk (half-duplex walkie-talkie) channel for the native app.

> **Not sure which to pick?** Use **Conference page** — it works on every registered phone and softphone with no per-phone setup. Multicast is a LAN latency optimization.

## Create a paging group

1. Go to **Messaging ▾ → Paging / PTT**.
2. In **New paging group**, enter:
   - **Name** (for example "All staff").
   - **Page code** — a dialable extension (for example `800`). Dialing this pages the group.
   - **Mode** — Conference page, Multicast, or Native PTT.
   - For Multicast, also set the **Multicast address** and **Multicast port**.
3. Click **Create group**.

## Add members

1. In the **Groups** table, click **Manage →** on the group.
2. In the member area, pick an extension from **Add an extension…** and click **Add member**.
3. Repeat for everyone who should hear the page.

For a Conference-page group, dialing the page code now pages all members. For Multicast, the page reaches phones listening on the configured address.

## Enable, disable, or delete a group

Use the **Disable** / **Enable** and **Delete group** buttons on the group's management panel.

## Send a page from your phone

To record and broadcast a page from a phone or computer instead of dialing the code, use the [Broadcast app](paging-broadcast-app.md).

## Related

- [The Broadcast app: voice blasts and live push-to-talk](paging-broadcast-app.md)
- [Create and manage extensions](extensions-create-and-manage.md)
