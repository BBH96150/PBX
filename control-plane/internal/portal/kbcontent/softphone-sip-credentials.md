# Connect a Softphone with SIP Credentials

You can register any standard SIP client (Linphone, Bria, Zoiper, baresip, or a hardware phone) to an extension using its SIP credentials. This is the manual alternative to zero-touch desk-phone provisioning.

## Get the credentials

1. Open the extension's detail page in the portal.
2. On the **SIP credentials for a softphone or desk phone** card, note:
   - **Server / Proxy**
   - **Port**
   - **Transport** (usually UDP)
   - **Domain / Realm** (for example `yourslug.pbx.tendpos.com`)
   - **Username** and **Auth user**
3. Click **Show password** to reveal the SIP password, then **Copy**.

## Enter them into your SIP app

The exact menu names vary by app, but you'll always need:

1. **Username / Auth user:** the SIP username from the credentials card.
2. **Password:** the SIP password you revealed.
3. **Domain / Realm:** the domain shown on the card — this must match exactly.
4. **Transport:** UDP (unless told otherwise).
5. **Proxy / Server:** the server and port shown on the card.

### Linphone quick steps

1. Open Linphone → Settings → Account assistant → **Use a SIP account**.
2. Enter the username and domain from the credentials card.
3. Set the display name.
4. Set proxy/transport to the server, port, and transport shown.
5. Paste the password.
6. Test by dialing `9999` — you should hear your own voice echoed back.

## Important notes

- The **domain/realm must match exactly**, or registration will fail with an authentication error.
- If you rotate the SIP password in the portal, you must update it in the app or the phone will be kicked off.
- On iOS, third-party SIP apps often drop their registration when backgrounded — this is an iOS limitation. For dependable calling, use a desk phone.

## Related

- [Create and manage extensions](extensions-create-and-manage.md)
- [Use the web softphone](softphone-web.md)
- [Troubleshooting & FAQ](troubleshooting-faq.md)
