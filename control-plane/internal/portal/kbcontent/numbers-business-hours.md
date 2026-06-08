# Business Hours and After-Hours Routing

A schedule is a set of weekly open hours plus holiday overrides, in a specific timezone. Attach it to a phone number to route calls differently when you're closed.

## Create a schedule

1. Go to **Numbers ▾ → Business hours**.
2. In **Add a schedule**, enter:
   - **Name** — for example "Office hours".
   - **Timezone** — an IANA timezone such as `America/New_York` or `Europe/London`.
3. Click **Create**.

## Set the open hours

1. Click **Manage hours →** next to the schedule.
2. Add one or more **periods** — the days and times you're open.
3. Add **holidays** for dates that should be treated as closed regardless of the weekly pattern.

## Apply it to a phone number

1. Go to **Numbers ▾ → Phone numbers**.
2. Expand **After-hours routing** on the number's row.
3. Choose the **Schedule**.
4. Set **When closed, route to** — for example a voicemail box or an after-hours greeting.
5. Click **Save after-hours**.

During open hours the number uses its normal destination; outside them it uses the closed destination.

## Related

- [Route inbound phone numbers (DIDs)](numbers-route-dids.md)
- [Set up voicemail and voicemail-to-email](voicemail-setup.md)
- [IVR menus](calling-ivr-menus.md)
