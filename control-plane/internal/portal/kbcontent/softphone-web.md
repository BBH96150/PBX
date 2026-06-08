# Use the Web Softphone

The web softphone lets you place and receive calls right in your browser, using an extension assigned to your account — no separate app to install.

## Open the softphone

1. Sign in at **https://pbx.tendpos.com/admin**.
2. Go to **https://pbx.tendpos.com/admin/softphone**.

> If you see "No extension assigned," ask a tenant admin to attach an extension to your user account, then come back.

## Connect

1. Choose the extension you want to use from the **Extension** dropdown.
2. Click **Open softphone**. The page issues temporary SIP credentials and registers your browser.
3. When you see **registered as …**, the dialer appears.

## Place a call

1. In the **Number / Extension** box, type an internal extension (for example `1002`) or an outside number in +E.164 format (for example `+14155551234`).
2. Click **Call**.
3. Use **Hang up** to end the call and **Mute** to mute your microphone.

Incoming calls to your extension are accepted automatically while the softphone is open.

## If you don't hear audio

The web softphone's signaling (ringing, call setup) works end to end, but two-way audio depends on your network and the server's media configuration. If calls connect but you hear silence, see the audio section in [Troubleshooting & FAQ](troubleshooting-faq.md). The **Diagnostics** panel at the bottom of the page logs what's happening and is helpful when reporting an issue.

## Related

- [Connect a softphone with SIP credentials](softphone-sip-credentials.md)
- [Manage your own phone (self-service)](self-service-portal.md)
- [Troubleshooting & FAQ](troubleshooting-faq.md)
