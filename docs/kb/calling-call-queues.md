# Call Queues

A call queue holds callers in line and connects them to agents as they become free — ideal for support and sales lines where calls can outnumber available people. Unlike a ring group, a queue keeps callers waiting (with music on hold) instead of dropping them when everyone is busy.

## Create a queue

1. Open your workspace **Overview** page and find the queues section.
2. Enter:
   - **Name** (for example "Support").
   - **Extension** — the internal number to dial it (optional; leave blank for a DID-only queue).
   - **Strategy** (see below).
3. Create it, then open it to add agents.

## Distribution strategies

- **longest-idle-agent** — the agent who's been free the longest.
- **round-robin** — rotates through agents.
- **top-down** — starts from the top of the agent list each time.
- **ring-all** — rings every available agent at once.
- **random** — picks an available agent at random.
- **sequentially-by-agent-order** — follows the agent order you set.
- **agent-with-least-talk-time** / **agent-with-fewest-calls** — load-balancing options.

## Add agents

1. Open the queue's detail page.
2. In **Add an agent**, choose an extension and set:
   - **Tier** — lower tiers answer first.
   - **Position** — orders agents within a tier.
   - **Wrap-up (sec)** — protected time after a call before the agent gets the next one.
3. Click **Add agent**.

A queue with no agents will hold callers but never connect them.

## Edit settings

Expand **Edit settings** to change the name, extension, strategy, **Music on hold**, and **Max wait** (how long a caller waits before the queue gives up).

## Watch the queue live

See [The live queue board](reports-queue-board.md) to monitor waiting callers and agent status in real time.

## Related

- [The live queue board](reports-queue-board.md)
- [Music on hold](calling-music-on-hold.md)
- [Ring groups](calling-ring-groups.md)
