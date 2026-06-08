# Add and Provision Desk Phones

Desk phones are the recommended choice for reliable, always-on calling. The platform supports zero-touch provisioning (ZTP) for **Yealink**, **Polycom**, and **Grandstream** phones — the phone fetches its own configuration at boot, so you never type credentials into the handset.

## How zero-touch provisioning works

1. You add the phone to the portal by its **MAC address** and bind one or more extensions to its line keys.
2. The phone boots and requests its config from the provisioning server.
3. It signs itself in and registers — no manual SIP setup on the phone.

## Add a device

1. Open your workspace **Overview** page and find the **Devices** section.
2. Enter the phone's:
   - **MAC address** (printed on a sticker on the phone or its box).
   - **Vendor** (Yealink, Polycom, or Grandstream).
   - **Model**.
   - **Label** (optional, for your own reference).
3. Click to add the device, then open it to bind lines.

## Bind a line to an extension

A phone won't provision until at least one line is bound — a device with no lines is intentionally rejected.

1. Open the device's detail page.
2. In **Bind a line**, choose:
   - **Line number** — which physical line key on the phone (1 = primary).
   - **Extension** — which extension that key signs in as.
   - **Label** (optional, for example "Front desk").
3. Click **Bind line**. Repeat for additional line keys.

## Confirm the phone provisioned

On the device page, check:

- **Last provisioned** — shows the date/time and source IP once the phone has fetched its config.
- **RPS** — shows **synced** when redirection/registration to our server completed (Yealink/Polycom redirection service), or **pending** until then.

## Remove a line or a device

- Use **Unbind** next to a line to free that line key.
- Use **Delete device** to remove the phone entirely and unbind all of its lines.

## Related

- [Create and manage extensions](extensions-create-and-manage.md)
- [Connect a softphone with SIP credentials](softphone-sip-credentials.md)
- [Troubleshooting & FAQ](troubleshooting-faq.md)
