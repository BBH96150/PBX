# Ring Groups

A ring group rings several extensions when a call comes in — perfect for "ring the whole sales team" or "ring the front desk and the back office together." A call reaches a ring group either by dialing its extension or by routing a phone number to it.

## Create a ring group

1. Open your workspace **Overview** page and find the ring groups section.
2. Enter:
   - **Name** (for example "Sales").
   - **Extension** — the internal number to dial it (optional; leave blank for a DID-only group).
   - **Strategy** (see below).
3. Create it, then open it to add members.

## Ring strategies

- **Simultaneous** — every member rings at once; first to answer wins.
- **Sequential** — members ring one after another, lowest priority first.
- **Round-robin** — spreads calls across members in rotation.
- **Random** — picks members in random order.

## Add members

1. Open the ring group's detail page.
2. In **Add a member**, choose an extension and set:
   - **Priority** — lower rings earlier (for sequential and round-robin).
   - **Ring delay (sec)** — wait this long before this member starts ringing.
3. Click **Add member**.

A ring group with no members can't ring anyone, so add at least one.

## Edit settings

Expand **Edit settings** on the ring group page to change the name, extension, strategy, **Ring timeout** (how long the group rings before giving up), and a **Caller-ID prefix** (for example `[Sales]`) that labels calls arriving through the group.

## Enable, disable, or delete

Use the buttons on the settings card to disable a group temporarily or delete it (which also removes its members).

## Related

- [Create and manage extensions](extensions-create-and-manage.md)
- [Call queues](calling-call-queues.md)
- [Route inbound phone numbers (DIDs)](numbers-route-dids.md)
