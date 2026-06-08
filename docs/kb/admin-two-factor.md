# Two-Factor Authentication (2FA)

Two-factor authentication adds a one-time code from an authenticator app on top of your password, so a stolen password isn't enough to get in.

## Set up an authenticator

1. Go to **Account → 2FA** (at `/admin/security/2fa`).
2. Optionally name the **Device label** (for example "iPhone (Authy)").
3. Click **Set up authenticator**.
4. Scan the QR code (or enter the key) with an authenticator app such as Google Authenticator, Authy, or 1Password.
5. Enter the 6-digit code to confirm.
6. **Save your recovery codes** somewhere safe — they let you get in if you lose your phone.

## Signing in with 2FA on

After your password, you'll be prompted for a 6-digit code. Check **Trust this browser** on that screen to skip the prompt on that device for 30 days.

## Workspace-required 2FA

A workspace can require 2FA for all members. If yours does and you haven't enrolled, you'll be sent to the 2FA page to set it up before you can continue.

## Manage trusted devices

On the 2FA page, the **Trusted devices** list shows browsers you've trusted. Click **Revoke** to remove one (for example a shared or lost computer).

## Disable 2FA

Expand **Disable 2FA** and supply a current 6-digit code or a recovery code to confirm.

> If 2FA options don't appear, the platform operator hasn't enabled the feature on this deployment.

## Related

- [Your account: profile, password, and security](admin-account-and-password.md)
- [Manage active sessions](admin-sessions.md)
