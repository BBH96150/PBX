// Package webhook delivers signed outbound event callbacks to tenant-configured
// HTTP endpoints. Delivery is best-effort and asynchronous so it never blocks
// the call path. All requests go through an SSRF guard that refuses to connect
// to non-public addresses (loopback/private/link-local), since the control
// plane can otherwise reach internal services (Postgres, Redis, the FS ESL).
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

type Dispatcher struct {
	store  *store.Store
	client *http.Client
}

// New builds a Dispatcher whose HTTP client blocks connections to non-public
// IPs at dial time (DNS-rebind safe — the check is on the resolved address).
func New(st *store.Store) *Dispatcher {
	dialer := &net.Dialer{
		Timeout: 4 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			return guardAddress(address)
		},
	}
	return &Dispatcher{
		store: st,
		client: &http.Client{
			Timeout:   6 * time.Second,
			Transport: &http.Transport{DialContext: dialer.DialContext},
			// Don't follow redirects — a 30x could bounce to an internal URL.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// guardAddress rejects connections to non-public IP literals.
func guardAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("webhook: unresolved address %q", address)
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || !ip.IsGlobalUnicast() {
		return fmt.Errorf("webhook: blocked non-public address %s", ip)
	}
	return nil
}

// Fire asynchronously delivers an event to every enabled endpoint in the tenant
// subscribed to it. Returns immediately; failures are logged, not surfaced.
func (d *Dispatcher) Fire(tenantID uuid.UUID, event string, payload map[string]any) {
	if d == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		eps, err := d.store.ListEnabledWebhookEndpointsForEvent(ctx, tenantID, event)
		if err != nil {
			slog.Error("webhook: list endpoints", "event", event, "err", err)
			return
		}
		for _, ep := range eps {
			if err := d.DeliverOne(ctx, ep, event, payload); err != nil {
				slog.Warn("webhook delivery failed", "url", ep.URL, "event", event, "err", err)
			}
		}
	}()
}

// DeliverOne signs and POSTs one event to one endpoint (synchronous). Exported
// so the portal "test" button can exercise a single endpoint.
func (d *Dispatcher) DeliverOne(ctx context.Context, ep store.WebhookEndpoint, event string, payload map[string]any) error {
	body, err := json.Marshal(map[string]any{
		"event":   event,
		"sent_at": time.Now().UTC().Format(time.RFC3339),
		"data":    payload,
	})
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, []byte(ep.Secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "SIP-Platform-Webhook/1")
	req.Header.Set("X-Webhook-Event", event)
	req.Header.Set("X-Webhook-Signature", sig)
	req.Header.Set("X-Webhook-Id", ep.ID.String())

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("endpoint returned %d", resp.StatusCode)
	}
	return nil
}
