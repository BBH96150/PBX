package rps

import (
	"context"
	"log/slog"
)

// LogOnly is the "always works, never calls a vendor" provider. Useful as a
// configured fallback when an admin wants visibility into RPS attempts
// without setting up vendor accounts yet, and as the implicit fallback in
// Registry when no vendor adapter matches.
type LogOnly struct{ vendor string }

func NewLogOnly(vendor string) *LogOnly { return &LogOnly{vendor: vendor} }

func (l *LogOnly) Name() string { return l.vendor }

func (l *LogOnly) Register(_ context.Context, req RegisterRequest) error {
	slog.Info("rps[log_only] register",
		"vendor", req.Vendor, "model", req.Model, "mac", req.MAC,
		"tenant", req.TenantSlug, "url", req.ProvisioningURL,
	)
	return nil
}

func (l *LogOnly) Unregister(_ context.Context, mac string) error {
	slog.Info("rps[log_only] unregister", "vendor", l.vendor, "mac", mac)
	return nil
}
