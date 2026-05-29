package freeswitch

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// SyncQueueToFS pushes a queue and all its current agents/tiers into
// mod_callcenter via ESL commands. Called after admin API mutations.
//
// Best-effort: returns an error so the caller can log, but the API still
// returns success to the admin (the queue is persisted in our DB regardless).
// If ESL is not connected, returns ErrNotConnected and the admin can fall
// back to `reload mod_callcenter` once FS is reachable.
func (c *ESLClient) SyncQueueToFS(ctx context.Context, queueID uuid.UUID) error {
	cfg, err := c.store.LoadOneQueueConfig(ctx, queueID, c.kamailioTarget)
	if err != nil {
		return fmt.Errorf("load queue config: %w", err)
	}
	cmds := BuildQueueProvisionCommands(cfg)
	return c.runCommands(ctx, cmds)
}

// SyncAgentForExtension re-pushes every queue+tier this extension's agent
// is bound to. Called after AddQueueAgent.
func (c *ESLClient) SyncAgentForExtension(ctx context.Context, extensionID uuid.UUID) error {
	cfgs, err := c.store.LoadQueueConfigsForExtension(ctx, extensionID, c.kamailioTarget)
	if err != nil {
		return fmt.Errorf("load configs for ext: %w", err)
	}
	var allCmds []string
	for _, cfg := range cfgs {
		allCmds = append(allCmds, BuildQueueProvisionCommands(cfg)...)
	}
	return c.runCommands(ctx, allCmds)
}

func (c *ESLClient) runCommands(ctx context.Context, cmds []string) error {
	if len(cmds) == 0 {
		return nil
	}
	for _, cmd := range cmds {
		if err := c.callAPI(ctx, cmd); err != nil {
			return fmt.Errorf("%q: %w", cmd, err)
		}
		slog.Debug("esl provision", "cmd", cmd)
	}
	return nil
}

// BuildQueueProvisionCommands generates the imperative ESL command list that
// brings one queue + its agents + tiers into existence on mod_callcenter.
//
// Order matters:
//   1. queue load — re-fetches the queue definition from our dynamic
//      callcenter.conf and instantiates it
//   2. agent add — idempotent; creates the agent record if new (errors
//      ignored on next set call)
//   3. agent set ... — overrides each tunable param (contact, status, timers)
//   4. tier add — links each agent to this queue at its level/position
//
// Pure function — exported for unit tests.
func BuildQueueProvisionCommands(cfg *store.CallcenterConfig) []string {
	if cfg == nil || len(cfg.Queues) == 0 {
		return nil
	}

	var out []string
	for _, q := range cfg.Queues {
		out = append(out, fmt.Sprintf("callcenter_config queue load %s", q.Name))
	}
	for _, a := range cfg.Agents {
		out = append(out,
			fmt.Sprintf("callcenter_config agent add %s %s", a.Name, a.Type),
			fmt.Sprintf("callcenter_config agent set contact %s '%s'", a.Name, a.Contact),
			fmt.Sprintf("callcenter_config agent set status %s Available", a.Name),
			fmt.Sprintf("callcenter_config agent set max_no_answer %s %d", a.Name, a.MaxNoAnswer),
			fmt.Sprintf("callcenter_config agent set wrap_up_time %s %d", a.Name, a.WrapUpTime),
			fmt.Sprintf("callcenter_config agent set reject_delay_time %s %d", a.Name, a.RejectDelayTime),
			fmt.Sprintf("callcenter_config agent set busy_delay_time %s %d", a.Name, a.BusyDelayTime),
			fmt.Sprintf("callcenter_config agent set no_answer_delay_time %s %d", a.Name, a.NoAnswerDelayTime),
		)
	}
	for _, t := range cfg.Tiers {
		out = append(out, fmt.Sprintf("callcenter_config tier add %s %s %d %d",
			t.Queue, t.Agent, t.Level, t.Position))
	}
	return out
}
