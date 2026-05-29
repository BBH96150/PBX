// Package rps integrates with manufacturer redirection / provisioning
// services so that newly-shipped phones can boot, contact the vendor cloud,
// and get redirected to our provisioning HTTPS endpoint — no factory reset
// or manual URL entry needed.
//
// Each vendor has its own portal + API:
//   - Polycom ZTP        — https://api.ztp.poly.com
//   - Yealink RPS / YDMP — https://yms.yealink.com
//   - Grandstream GDMS   — https://www.gdms.cloud
//   - Cisco PSS / Webex  — varies by series
//
// Every adapter implements Provider. A Registry maps vendor strings (as
// stored on devices.vendor) to the right adapter. If a vendor has no
// adapter registered, the LogOnly fallback runs — the integration is a
// no-op but logs what would be sent, which is enough to ship the data
// model + control-plane plumbing before per-vendor credentials are in place.
package rps

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// RegisterRequest is everything an RPS adapter needs to bind a MAC to our
// provisioning service.
type RegisterRequest struct {
	MAC             string // colon form (aa:bb:cc:dd:ee:ff); adapters normalize as needed
	Vendor          string
	Model           string
	ProvisioningURL string // base URL phones should fetch from (vendor appends $MAC.cfg etc.)
	TenantSlug      string // for audit/log context
}

// Provider is a single vendor's RPS adapter.
type Provider interface {
	// Name returns the canonical vendor string (matches devices.vendor enum).
	Name() string

	// Register binds the MAC to our provisioning endpoint in the vendor's cloud.
	// Should be idempotent — admins re-create devices in our system; we want
	// the second call to succeed quietly rather than 409.
	Register(ctx context.Context, req RegisterRequest) error

	// Unregister removes the MAC from the vendor's cloud (called on device
	// delete). Should be tolerant of "not found".
	Unregister(ctx context.Context, mac string) error
}

// Registry routes incoming sync requests to the right Provider by vendor
// name. Lookups are case-insensitive on vendor. Unknown vendors fall
// through to a LogOnly adapter so the rest of the platform keeps working.
type Registry struct {
	providers map[string]Provider
	fallback  Provider
}

func NewRegistry(fallback Provider, providers ...Provider) *Registry {
	r := &Registry{
		providers: map[string]Provider{},
		fallback:  fallback,
	}
	for _, p := range providers {
		r.providers[strings.ToLower(p.Name())] = p
	}
	return r
}

// Register routes a request to the matching Provider; falls back to the
// LogOnly adapter if no match.
func (r *Registry) Register(ctx context.Context, req RegisterRequest) error {
	p := r.For(req.Vendor)
	return p.Register(ctx, req)
}

func (r *Registry) Unregister(ctx context.Context, vendor, mac string) error {
	p := r.For(vendor)
	return p.Unregister(ctx, mac)
}

// For returns the Provider that handles `vendor`, or the fallback.
func (r *Registry) For(vendor string) Provider {
	if r == nil {
		return defaultLogOnly{}
	}
	if p, ok := r.providers[strings.ToLower(vendor)]; ok {
		return p
	}
	if r.fallback != nil {
		return r.fallback
	}
	return defaultLogOnly{}
}

// IsTransientError is a hint to retry queues — for Wave A it's all-or-nothing,
// but the interface is here for adapters that want to distinguish.
type retryable interface{ Retry() bool }

func IsTransientError(err error) bool {
	if err == nil {
		return false
	}
	var r retryable
	if errAs(err, &r) {
		return r.Retry()
	}
	return false
}

// errAs is a tiny wrapper around errors.As so callers don't need to import
// errors just for the type-switch.
func errAs(err error, target any) bool {
	type unwrapper interface{ Unwrap() error }
	for err != nil {
		if a, ok := err.(retryable); ok {
			if t, ok := target.(*retryable); ok {
				*t = a
				return true
			}
		}
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// MACPlain returns the lowercase no-separator form most vendor APIs want
// (aa:bb:cc:dd:ee:ff → aabbccddeeff).
func MACPlain(s string) string {
	r := strings.NewReplacer(":", "", "-", "", ".", "", " ", "")
	return strings.ToLower(r.Replace(s))
}

// fmtErr is a small helper for adapters' error messages.
func fmtErr(vendor, op string, err error) error {
	return fmt.Errorf("rps[%s] %s: %w", vendor, op, err)
}

// defaultLogOnly is used when callers don't supply any fallback.
type defaultLogOnly struct{}

func (defaultLogOnly) Name() string { return "log_only" }
func (defaultLogOnly) Register(_ context.Context, req RegisterRequest) error {
	slog.Info("rps would register (no provider configured)",
		"vendor", req.Vendor, "mac", req.MAC, "url", req.ProvisioningURL)
	return nil
}
func (defaultLogOnly) Unregister(_ context.Context, mac string) error {
	slog.Info("rps would unregister (no provider configured)", "mac", mac)
	return nil
}
