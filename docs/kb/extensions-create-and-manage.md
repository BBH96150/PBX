# Create and Manage Extensions

An extension is an internal "phone" — a number like 101 plus the SIP credentials a desk phone or softphone uses to sign in. This article covers creating extensions, assigning owners, and finding SIP credentials.

## Create one extension

1. Open your workspace **Overview** page.
2. In the **Extensions** section, fill in:
   - **Extension number** (for example `101`).
   - **Display name** (for example `Alice` or `Front Desk`).
3. Click **Add extension**. SIP credentials are generated automatically.

## Create many extensions at once (bulk)

1. On the **Overview** page, find the bulk extension creation box.
2. Paste one extension per line. You can include a display name after a comma:
   ```
   101,Alice
   102,Bob
   103,Front Desk
   ```
3. Submit. The portal reports how many were created and how many were skipped (for example, duplicates).

## Find an extension's SIP credentials

1. Open the extension's detail page (click it from the Overview list).
2. The **SIP credentials for a softphone or desk phone** card shows the server, port, transport, domain/realm, and username.
3. Click **Show password** to reveal the SIP password, then **Copy** to copy it.

Hand the username, password, and domain to the user so they can sign their phone in. See [Connect a softphone with SIP credentials](softphone-sip-credentials.md).

## Assign an owner (gives self-service access)

Assigning an owner lets that person manage the extension's forwarding, do-not-disturb, and voicemail from their own self-service area.

1. On the extension detail page, expand **Owner (self-service access)**.
2. Pick the user from the **Owner** dropdown (invite them first from the **Team** page if they're not listed).
3. Click **Save owner**.

## Rename or delete an extension

- Expand **Rename / delete** on the extension page to change the display name.
- The extension number and SIP identity can't be changed — delete and recreate if you need a different number.
- Before deleting, reassign any phone numbers (DIDs) that route to the extension, or the delete will be blocked.

## Rotate the SIP password

Click **Rotate password** on the extension page to issue a new SIP password. Any phone currently signed in with the old password will be disconnected and must re-register with the new one.

## Export your extensions

From the workspace you can download an **extensions CSV** — a provisioning sheet listing numbers, names, SIP usernames, and feature flags (no passwords).

## Related

- [Set call-handling features (forwarding, DND, voicemail)](calling-forwarding-and-dnd.md)
- [Connect a softphone with SIP credentials](softphone-sip-credentials.md)
- [Add and provision desk phones](devices-provision-desk-phones.md)
