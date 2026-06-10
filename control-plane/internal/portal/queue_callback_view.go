package portal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// queueCallbackList renders the per-tenant "Queue callbacks" page: pending and
// recent "keep your place in line" requests with Call-now / Cancel actions.
// Lives under the Calling ▾ subnav (NavActive "callbacks"). Mirrors the
// conference page shape.
func (s *Server) queueCallbackList(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	callbacks, _ := s.store.ListQueueCallbacksForTenant(r.Context(), tid, "")

	data := map[string]any{
		"Tenant":    tenant,
		"NavActive": "callbacks",
		"Callbacks": callbacks,
		"DialReady": s.callbackDialReady(),
	}
	s.renderLayout(w, r, tenant.Name+" · Queue callbacks", "queue_callbacks", data)
}

// queueCallbackCancel marks a callback cancelled (a manual terminal stop).
func (s *Server) queueCallbackCancel(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/callbacks"
	if err := s.store.CancelQueueCallback(r.Context(), tid, id); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "queue_callback.cancelled", "queue_callback", &id, nil)
	http.Redirect(w, r, redirect+"?flash=Callback+cancelled.", http.StatusSeeOther)
}

// queueCallbackDial is the "Call now" action: originate the caller via the
// tenant's primary carrier gateway and, on answer, &transfer them into the
// queue extension so they re-enter the queue for the next agent.
func (s *Server) queueCallbackDial(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/callbacks"

	cb, err := s.store.GetQueueCallbackForTenant(r.Context(), tid, id)
	if err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	if err := s.dialQueueCallback(r.Context(), cb); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "queue_callback.dialed", "queue_callback", &id, map[string]any{
		"caller": cb.CallerNumber, "queue_id": cb.QueueID.String(),
	})
	http.Redirect(w, r, redirect+"?flash=Calling+back+now.", http.StatusSeeOther)
}

// callbackDialReady reports whether the callback dialer is wired (an originator
// + a SIP routing target to build fs_path with).
func (s *Server) callbackDialReady() bool {
	return s.originator != nil && s.sipRoutingTarget != ""
}

// dialQueueCallback originates the caller via the tenant's primary carrier
// gateway and transfers the answered leg into the queue extension. It marks the
// row dialing→connected/failed and increments the attempt counter. Shared by
// the manual "Call now" button and the background dialer.
func (s *Server) dialQueueCallback(ctx context.Context, cb *store.QueueCallback) error {
	if !s.callbackDialReady() {
		return httpErr("callback dialer not configured on this server")
	}
	if cb.QueueExtension == "" {
		return httpErr("queue has no dialable extension to transfer the caller into")
	}

	// Build the carrier dial string the same way outbound/E911 do: pick the
	// tenant's primary trunk and dial sofia/gateway/<fs_gateway_name>/<number>.
	acct, err := s.store.PickPrimaryCarrierAccountForTenant(ctx, cb.TenantID)
	if err != nil {
		return httpErr("no carrier trunk configured for this tenant")
	}

	// Mark the attempt before originating so a crash mid-dial doesn't leave the
	// row pending forever (it lands in 'dialing' and won't be re-picked as
	// pending). Cap attempts at 3 — beyond that the row is marked failed.
	if cb.Attempts >= 3 {
		_ = s.store.SetQueueCallbackStatus(ctx, cb.TenantID, cb.ID, "failed")
		return httpErr("callback has reached the maximum of 3 attempts")
	}
	if err := s.store.IncrementQueueCallbackAttempt(ctx, cb.TenantID, cb.ID, "dialing"); err != nil {
		return err
	}

	dialString := fmt.Sprintf("sofia/gateway/%s/%s", acct.FSGatewayName, cb.CallerNumber)
	// On answer, transfer the leg into the queue extension (XML default context)
	// so the caller re-enters the queue and reaches the next agent.
	app := fmt.Sprintf("&transfer(%s XML default)", cb.QueueExtension)

	if _, err := s.originator.Originate(ctx, dialString, app); err != nil {
		// Originate failed → back to pending so it can be retried (until the cap).
		_ = s.store.SetQueueCallbackStatus(ctx, cb.TenantID, cb.ID, "pending")
		return httpErr("could not place the callback: " + err.Error())
	}

	// Originate accepted by FreeSWITCH — mark connected. (Whether the caller
	// actually answered is observed out-of-band via CDRs; for the control-plane
	// lifecycle "connected" means the call was successfully placed.)
	_ = s.store.SetQueueCallbackStatus(ctx, cb.TenantID, cb.ID, "connected")
	return nil
}

// RunCallbackDialer is a small background worker: every `interval` it pops
// pending callbacks and dials them (marking dialing→connected/failed and
// incrementing attempts, capped at 3 inside dialQueueCallback). It blocks until
// ctx is canceled. No-op when the dialer isn't wired (no originator / SIP
// target). Mirrors the TrunkMonitor loop shape.
func (s *Server) RunCallbackDialer(ctx context.Context, interval time.Duration) {
	if !s.callbackDialReady() {
		return
	}
	if interval <= 0 {
		interval = 20 * time.Second
	}
	t := time.NewTimer(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.dialPendingCallbacks(ctx)
			t.Reset(interval)
		}
	}
}

// dialPendingCallbacks walks every tenant's oldest pending callback and attempts
// it. Errors are best-effort; one bad callback never blocks the others.
func (s *Server) dialPendingCallbacks(ctx context.Context) {
	if !s.callbackDialReady() {
		return
	}
	pending, err := s.store.ListAllPendingQueueCallbacks(ctx)
	if err != nil {
		return
	}
	// One attempt per tenant per pass to avoid hammering a single trunk.
	donePerTenant := map[uuid.UUID]bool{}
	for i := range pending {
		cb := pending[i]
		if donePerTenant[cb.TenantID] {
			continue
		}
		donePerTenant[cb.TenantID] = true
		if err := s.dialQueueCallback(ctx, &cb); err != nil &&
			!errors.Is(err, store.ErrCrossTenant) {
			// best-effort; the row's status reflects the outcome.
			continue
		}
	}
}
