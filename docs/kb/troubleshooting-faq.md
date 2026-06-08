# Troubleshooting & FAQ

Quick fixes for the most common issues. If something here doesn't resolve it, contact your administrator or account support with the details (extension number, phone number, and what you were doing).

## My desk phone or softphone won't register

- **Double-check the credentials.** Username, password, and **domain/realm** must match the extension exactly. The domain is usually `<your-slug>.pbx.tendpos.com`. See [Connect a softphone with SIP credentials](softphone-sip-credentials.md).
- **The realm/domain must match exactly** or you'll get an authentication failure. If you renamed a domain or rotated credentials, re-enter the current values.
- **Did you rotate the SIP password?** Rotating it disconnects phones using the old one — update the phone with the new password.
- **Desk phone (ZTP):** make sure the device has at least one **line bound** to an extension — a phone with no lines won't provision. Check **Last provisioned** on the device page. See [Add and provision desk phones](devices-provision-desk-phones.md).
- **iOS softphones** often drop registration when backgrounded. This is an iOS limitation; use a desk phone for always-on reliability.

## Inbound calls go to the carrier's voicemail instead of ringing my phones

This is almost always a setting on the carrier side:

- Make sure the carrier extension type is a **SIP / IP phone** (not "web softphone" or "voicemail only").
- Set the carrier's **voicemail pickup delay to at least 30 seconds** — a low value steals the call before it reaches us.
- Make sure **no other device** is registered as the same carrier account (an old phone or a leftover app will intercept calls).
- For specific DIDs, confirm each number is **forwarded to the right extension** in the carrier portal.

## My trunk shows FAIL_WAIT (not REGED)

- Recheck the **SIP username and password** in the carrier portal — they're separate from your web login.
- For CallCentric, use the correct username format (main account, or main + `*` + sub-account). See [Connect a carrier (SIP trunk)](trunks-connect-carrier.md).
- If credentials are correct but you get "403 Incorrect Authentication," your carrier may have assigned you a specific server — set a **Proxy host override** under **Advanced**.

## Outbound calls fail immediately

- Confirm your trunk shows **REGED** on the Trunks page.
- Check your **outbound routing** — if you've added rules, make sure one matches the number you're dialing (or that your default trunk is healthy). See [Outbound calling routes](numbers-outbound-routing.md).
- Verify the registering phone's domain/realm matches the workspace domain exactly.

## No audio on web softphone calls (it rings but is silent)

- The web softphone's call setup works, but two-way audio depends on your network and the server's media configuration. If calls connect silently, check the **Diagnostics** panel at the bottom of the softphone page and share it when reporting the issue.
- Allow microphone access in your browser when prompted.
- Try a wired or stronger network connection — restrictive firewalls/NAT can block media.

## I didn't receive a page (paging / broadcast)

- Confirm you're a **member** of the paging group. See [Paging groups](paging-groups.md).
- The group and its members must be **enabled**.
- For **Conference page** groups, members must be registered phones/softphones (they auto-answer the page).
- For **Multicast** groups, phones must be on the same LAN and provisioned with the multicast address.
- For **Live push-to-talk** in the [Broadcast app](paging-broadcast-app.md), you need an extension assigned to your account.

## Voicemail-to-email isn't arriving

- Make sure the voicemail box has an **email address** set. See [Set up voicemail and voicemail-to-email](voicemail-setup.md).
- Check spam/junk folders.
- Email delivery depends on the platform's mail configuration — if multiple users aren't getting any email (invites, resets, digests), contact the operator.

## I'm stuck on the 2FA / verify-email screen

- **2FA:** your workspace may require 2FA. Enroll an authenticator to continue. See [Two-factor authentication](admin-two-factor.md). Lost your device? Use a recovery code, or ask an admin to reset your 2FA.
- **Verify email:** your workspace may require a verified email. Use the verification link sent to your inbox, or ask an admin to resend it.

## A teammate can only see "My extensions" and nothing else

That account has the **user** role, which is confined to the self-service area. To give them management access, an admin can re-invite or set them as **tenant_admin**. See [Invite your team and set roles](admin-team-and-roles.md).

## Related

- [First-time setup checklist](first-time-setup.md)
- [Connect a carrier (SIP trunk)](trunks-connect-carrier.md)
- [Connect a softphone with SIP credentials](softphone-sip-credentials.md)
