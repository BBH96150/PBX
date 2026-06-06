package freeswitch

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/smtp"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
	"github.com/tendpos/sip-platform/control-plane/internal/webhook"
)

// TrunkMonitor periodically checks every enabled, registering trunk's gateway
// registration and emits an alert when one drops (REGED -> not) and a recovery
// notice when it comes back. State is tracked in memory; it alerts once per
// transition (and once for a trunk first observed down).
type TrunkMonitor struct {
	store      *store.Store
	gw         *GatewayProvisioner
	mailer     smtp.Mailer
	webhooks   *webhook.Dispatcher
	alertEmail string
	interval   time.Duration
	states     map[string]string // fs_gateway_name -> last observed state
}

func NewTrunkMonitor(st *store.Store, gw *GatewayProvisioner, mailer smtp.Mailer, wh *webhook.Dispatcher, alertEmail string, interval time.Duration) *TrunkMonitor {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &TrunkMonitor{
		store: st, gw: gw, mailer: mailer, webhooks: wh, alertEmail: alertEmail,
		interval: interval, states: map[string]string{},
	}
}

// Run blocks until ctx is canceled, checking trunk state every interval. A
// short initial delay lets FreeSWITCH register gateways after a boot/deploy so
// we don't alert on a not-yet-settled startup state.
func (m *TrunkMonitor) Run(ctx context.Context) {
	if m.gw == nil {
		slog.Info("trunk monitor disabled (no gateway provisioner)")
		return
	}
	t := time.NewTimer(45 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.check(ctx)
			t.Reset(m.interval)
		}
	}
}

func (m *TrunkMonitor) check(ctx context.Context) {
	accts, err := m.store.ListAllEnabledCarrierAccounts(ctx)
	if err != nil {
		slog.Warn("trunk monitor: list accounts", "err", err)
		return
	}
	for _, a := range accts {
		if !a.Register {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		st := m.gw.GatewayStatus(cctx, a.FSGatewayName)
		cancel()
		cur := st.State
		if !st.Found || cur == "" {
			cur = "DOWN"
		}
		prev, seen := m.states[a.FSGatewayName]
		m.states[a.FSGatewayName] = cur

		reged := cur == "REGED"
		wasReged := prev == "REGED"
		switch {
		case !reged && (!seen || wasReged):
			m.notify(ctx, a, prev, cur, true)
		case reged && seen && !wasReged:
			m.notify(ctx, a, prev, cur, false)
		}
	}
}

func (m *TrunkMonitor) notify(ctx context.Context, a store.CarrierAccount, oldState, newState string, down bool) {
	name := "—"
	var tenantAlertEmail string
	if a.TenantID != nil {
		if t, err := m.store.GetTenant(ctx, *a.TenantID); err == nil {
			name = t.Name
			tenantAlertEmail = t.AlertEmail
		}
	}
	if oldState == "" {
		oldState = "(unknown)"
	}
	slog.Warn("trunk alert",
		"down", down, "tenant", name, "trunk", a.Name,
		"gateway", a.FSGatewayName, "prev", oldState, "state", newState)

	if a.TenantID != nil {
		event := "trunk.up"
		if down {
			event = "trunk.down"
		}
		m.webhooks.Fire(*a.TenantID, event, map[string]any{
			"trunk":      a.Name,
			"carrier":    a.CarrierKind,
			"gateway":    a.FSGatewayName,
			"prev_state": oldState,
			"state":      newState,
		})
	}

	if !m.mailer.Configured() {
		return
	}
	recipients := m.alertRecipients(ctx, a.TenantID, tenantAlertEmail)
	if len(recipients) == 0 {
		return
	}
	var subject string
	if down {
		subject = fmt.Sprintf("⚠ Trunk DOWN: %s / %s", name, a.Name)
	} else {
		subject = fmt.Sprintf("✓ Trunk recovered: %s / %s", name, a.Name)
	}
	body := fmt.Sprintf(
		"Workspace: %s\nTrunk: %s (%s)\nGateway: %s\nRegistration: %s -> %s\nTime: %s\n",
		name, a.Name, a.CarrierKind, a.FSGatewayName, oldState, newState,
		time.Now().UTC().Format(time.RFC1123),
	)
	for _, to := range recipients {
		if err := m.mailer.Send(to, subject, body, nil); err != nil {
			slog.Error("trunk alert email", "to", to, "err", err)
		}
	}
}

// alertRecipients resolves who gets a tenant's trunk alerts, in priority order:
// the per-tenant override address, else the tenant's admins, else the global
// ALERT_EMAIL fallback.
func (m *TrunkMonitor) alertRecipients(ctx context.Context, tenantID *uuid.UUID, override string) []string {
	if override != "" {
		return []string{override}
	}
	if tenantID != nil {
		if admins, err := m.store.ListAdminEmailsForTenant(ctx, *tenantID); err == nil && len(admins) > 0 {
			return admins
		}
	}
	if m.alertEmail != "" {
		return []string{m.alertEmail}
	}
	return nil
}
