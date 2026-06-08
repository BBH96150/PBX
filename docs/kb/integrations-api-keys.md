# API Keys

API keys are workspace-scoped bearer tokens for the REST API (`/v1`). Use them to let an integration — for example TendPOS — read your workspace's extensions, call records, and directory, or make changes programmatically. Find them under **Admin ▾** for your workspace (the **API keys** page).

## Create a key

1. Open the **API keys** page for your workspace.
2. In **Create a key**, enter:
   - **Name** (for example "TendPOS integration").
   - **Scope**:
     - **read** (recommended) — view data only.
     - **write** — create and modify.
     - **admin** — full control.
3. Click **Create key**.
4. **Copy the key immediately** — it's shown only once.

## Use a key

Send it as a header on your API requests:

```
Authorization: Bearer <your-key>
```

## Review and revoke

The **Keys** table shows each key's name, prefix, scope, last-used time, and creation date. Click **Revoke** to disable a key — any integration using it stops working immediately.

## Related

- [Outbound webhooks](integrations-webhooks.md)
- [Manage active sessions](admin-sessions.md)
