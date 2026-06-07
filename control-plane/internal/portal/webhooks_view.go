package portal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// WebhookDeliverer is the slice of the webhook dispatcher the portal needs (for
// the per-endpoint "test" button).
type WebhookDeliverer interface {
	DeliverOne(ctx context.Context, ep store.WebhookEndpoint, event string, payload map[string]any) (int, error)
}

// webhookEventTypes are the events an endpoint can subscribe to.
var webhookEventTypes = []string{"call.completed", "trunk.down", "trunk.up", "voicemail.new"}

func (s *Server) webhooksList(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	eps, _ := s.store.ListWebhookEndpointsForTenant(r.Context(), tid)
	s.renderLayout(w, r, tenant.Name+" · Webhooks", "webhooks", map[string]any{
		"Tenant":     tenant,
		"NavActive":  "webhooks",
		"Endpoints":  eps,
		"EventTypes": webhookEventTypes,
		"Flash":      r.URL.Query().Get("flash"),
		"FlashErr":   r.URL.Query().Get("err"),
	})
}

func (s *Server) webhookCreate(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/webhooks"
	url := strings.TrimSpace(r.FormValue("url"))
	if !strings.HasPrefix(url, "https://") {
		http.Redirect(w, r, redirect+"?err=URL+must+start+with+https://", http.StatusSeeOther)
		return
	}
	// Only persist recognized event types; none selected = all events.
	var events []string
	for _, e := range r.Form["events"] {
		for _, valid := range webhookEventTypes {
			if e == valid {
				events = append(events, e)
			}
		}
	}
	secret := "whsec_" + randomHex(24)
	ep, err := s.store.CreateWebhookEndpoint(r.Context(), tid, url, secret, events)
	if err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "webhook.created", "webhook_endpoint", &ep.ID, map[string]any{"url": url, "events": events})
	http.Redirect(w, r, redirect+"?flash=Webhook+created.+Save+the+signing+secret+shown+below.", http.StatusSeeOther)
}

func (s *Server) webhookDelete(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/webhooks"
	if err := s.store.DeleteWebhookEndpointForTenant(r.Context(), tid, id); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "webhook.deleted", "webhook_endpoint", &id, nil)
	http.Redirect(w, r, redirect+"?flash=Webhook+deleted.", http.StatusSeeOther)
}

func (s *Server) webhookToggle(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/webhooks"
	enabled := r.FormValue("enabled") == "true"
	if err := s.store.SetWebhookEnabled(r.Context(), tid, id, enabled); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "webhook.enabled.set", "webhook_endpoint", &id, map[string]any{"enabled": enabled})
	http.Redirect(w, r, redirect+"?flash=Webhook+updated.", http.StatusSeeOther)
}

func (s *Server) webhookRotateSecret(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/webhooks"
	if err := s.store.RotateWebhookSecret(r.Context(), tid, id, "whsec_"+randomHex(24)); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "webhook.secret.rotated", "webhook_endpoint", &id, nil)
	http.Redirect(w, r, redirect+"?flash=Signing+secret+rotated+(update+your+receiver).", http.StatusSeeOther)
}

func (s *Server) webhookTest(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/webhooks"
	ep, err := s.store.GetWebhookEndpointForTenant(r.Context(), tid, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if s.webhooks == nil {
		http.Redirect(w, r, redirect+"?err=Webhook+delivery+not+available", http.StatusSeeOther)
		return
	}
	if _, err := s.webhooks.DeliverOne(r.Context(), *ep, "test.ping", map[string]any{
		"message": "This is a test event from your PBX.",
	}); err != nil {
		http.Redirect(w, r, redirect+"?err=Test+delivery+failed:+"+httpEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, redirect+"?flash=Test+event+delivered+(2xx+received).", http.StatusSeeOther)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func httpEscape(s string) string {
	return strings.NewReplacer(" ", "+", "&", "%26", "?", "%3F", "#", "%23").Replace(s)
}
