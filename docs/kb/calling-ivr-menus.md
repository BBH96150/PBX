# IVR Menus (Auto-Attendant)

An IVR menu greets callers and lets them press a digit to reach the right place — "Press 1 for Sales, 2 for Support." A call reaches an IVR by dialing its extension or by routing a phone number to it.

## Create an IVR

1. Open your workspace **Overview** page and find the IVR section.
2. Enter:
   - **Name** (for example "Main menu").
   - **Extension** — the internal number to dial it (optional; leave blank for a DID-only menu).
3. Create it, then open it to add menu options.

## Add menu options

1. Open the IVR's detail page.
2. In **Add an option**, set:
   - **Digit(s) the caller presses** — for example `1`.
   - **Label** (optional) — for example "Sales".
   - **Goes to** — the destination: an extension, ring group, queue, another IVR, or voicemail box. You can also choose **Dial an external number** or **Hang up**.
   - **External number** — only needed if you chose "Dial an external number."
3. Click **Add option**. Repeat for each digit.

## Tune the greeting and timing

Expand **Edit settings** to set:

- **Long greeting** and **Short greeting** sound paths (the prompts callers hear).
- **Timeout** and **Inter-digit timeout** (how long the system waits for input).
- **Digit length** (how many digits an option expects).

## Enable, disable, or delete

Use the buttons on the settings card. Deleting an IVR also removes all of its menu options.

## Related

- [Route inbound phone numbers (DIDs)](numbers-route-dids.md)
- [Ring groups](calling-ring-groups.md)
- [Set up voicemail and voicemail-to-email](voicemail-setup.md)
