# Single Sign-On (OIDC and SAML)

Let your team sign in with your existing identity provider (Google Workspace, Microsoft Entra/Azure AD, Okta, and others). The platform supports both **OIDC** and **SAML 2.0**, configured per workspace. New users are created automatically on first sign-in (just-in-time provisioning).

## OIDC SSO

### Configure it

1. Go to **Admin ▾ → OIDC SSO**.
2. Fill in the details from your identity provider (IdP):
   - **Label** (for example "Acme Google Workspace").
   - **Issuer URL** (for example `https://accounts.google.com`).
   - **Client ID** and **Client secret** from the app you register in your IdP.
   - **Scopes** (default `openid email profile`).
3. Register this **Callback URL** in your IdP: `https://pbx.tendpos.com/admin/sso/callback`.
4. Check **Enabled** and click **Save**.
5. Use **Test discovery** to confirm the issuer is reachable.

### Match email domains

Add your email **domains** (for example `acme.com`) so that when someone types a matching email on the login page, they get a **Sign in with SSO** button instead of a password field.

### Bookmarkable link

Share your per-workspace SSO URL — it skips the email lookup and goes straight to your IdP: `https://pbx.tendpos.com/admin/sso/<your-slug>/login`.

## SAML 2.0

Configure SAML under **Admin ▾ → SAML**. Enter your IdP's metadata/details, and provide your IdP with the platform's SP metadata (available from the SAML configuration page). Users then sign in through your IdP.

## Require SSO

Check **Require SSO for all members (block password login)** to force everyone in the workspace through your IdP. (Super-admins remain exempt so a misconfiguration can't lock everyone out.)

> If SSO options are unavailable, the platform operator hasn't enabled the feature on this deployment.

## Related

- [Invite your team and set roles](admin-team-and-roles.md)
- [Your account: profile, password, and security](admin-account-and-password.md)
