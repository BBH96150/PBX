# Manage Active Sessions

You can see everywhere you're signed in and sign out devices you no longer use — handy if you lose a laptop or sign in on a shared computer.

## View your sessions

1. Go to **Account → Active sessions** (at `/admin/security/sessions`).
2. The **Portal sessions** table lists each session with when it started, when it was last seen, a token prefix, and its scope. Your current session is tagged **this session**.

## Sign out a single session

Click **Revoke** next to any session other than your current one.

## Sign out everywhere else

Click **Sign out of all other sessions** to end every session except the one you're using now.

> Hand-issued API tokens aren't listed here — manage those under [API keys](integrations-api-keys.md).

## Related

- [Two-factor authentication (2FA)](admin-two-factor.md)
- [Your account: profile, password, and security](admin-account-and-password.md)
