package portal

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/audit"
	"github.com/tendpos/sip-platform/control-plane/internal/e164"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// didsList shows the per-tenant DID → destination mapping.
func (s *Server) didsList(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	dids, _ := s.store.ListDIDsForTenant(r.Context(), tid)
	exts := mustExtensions(r.Context(), s.store, tid)
	accounts, _ := s.store.ListCarrierAccountsForTenant(r.Context(), tid)

	// Build an ext-id → "101 — Alice" map for the DID rows so we don't
	// just show a UUID next to the phone number.
	extLabel := map[uuid.UUID]string{}
	for _, e := range exts {
		extLabel[e.ID] = e.Extension + " — " + e.DisplayName
	}

	s.renderLayout(w, r, tenant.Name+" · Phone numbers", "dids", map[string]any{
		"Tenant":     tenant,
		"DIDs":       dids,
		"Extensions": exts,
		"Accounts":   accounts,
		"ExtLabel":   extLabel,
	})
}

func (s *Server) didCreate(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	e164Raw := strings.TrimSpace(r.FormValue("e164"))
	normalized, err := e164.Normalize(e164Raw, "US")
	if err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/dids",
			errors.New("phone number must be E.164 format like +14155551234"))
		return
	}
	// Ensure it has a leading + (the DB check constraint requires it).
	if !strings.HasPrefix(normalized, "+") {
		normalized = "+" + normalized
	}

	extID, err := uuid.Parse(r.FormValue("extension_id"))
	if err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/dids",
			errors.New("pick an extension to route inbound calls to"))
		return
	}
	acctID, err := uuid.Parse(r.FormValue("carrier_account_id"))
	if err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/dids",
			errors.New("pick which trunk this number arrives on"))
		return
	}
	acct, err := s.store.GetCarrierAccountForTenant(r.Context(), tid, acctID)
	if err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/dids", err)
		return
	}

	did, err := s.store.CreateDID(r.Context(), store.CreateDIDInput{
		TenantID:         tid,
		CarrierID:        acct.CarrierID,
		CarrierAccountID: &acct.ID,
		E164:             normalized,
		DestinationKind:  "extension",
		DestinationID:    extID,
		CNAM:             strings.TrimSpace(r.FormValue("cnam")),
	})
	if err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/dids", err)
		return
	}

	ip, ua := audit.FromRequest(r)
	var actorTok *uuid.UUID
	if tok := tokenFromCtx(r.Context()); tok != nil {
		actorTok = &tok.ID
	}
	s.audit.Log(r.Context(), audit.Event{
		TenantID: &tid, ActorTokenID: actorTok,
		Event: "did.created", TargetType: "did", TargetID: &did.ID,
		IPAddress: ip, UserAgent: ua,
		Payload: map[string]any{"e164": normalized, "extension_id": extID.String()},
	})
	http.Redirect(w, r,
		"/admin/tenants/"+tid.String()+"/dids?flash="+normalized+"+routed+to+ext.",
		http.StatusSeeOther)
}

func (s *Server) didDelete(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	did, err := uuid.Parse(chi.URLParam(r, "didID"))
	if err != nil {
		http.Error(w, "bad did id", 400)
		return
	}
	if err := s.store.DeleteDIDForTenant(r.Context(), tid, did); err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/dids", err)
		return
	}
	ip, ua := audit.FromRequest(r)
	var actorTok *uuid.UUID
	if tok := tokenFromCtx(r.Context()); tok != nil {
		actorTok = &tok.ID
	}
	s.audit.Log(r.Context(), audit.Event{
		TenantID: &tid, ActorTokenID: actorTok,
		Event: "did.deleted", TargetType: "did", TargetID: &did,
		IPAddress: ip, UserAgent: ua,
	})
	http.Redirect(w, r, "/admin/tenants/"+tid.String()+"/dids?flash=Number+removed.", http.StatusSeeOther)
}

func (s *Server) didToggleEnabled(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	did, err := uuid.Parse(chi.URLParam(r, "didID"))
	if err != nil {
		http.Error(w, "bad did id", 400)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	enabled := r.FormValue("enabled") == "true"
	if err := s.store.SetDIDEnabledForTenant(r.Context(), tid, did, enabled); err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/dids", err)
		return
	}
	http.Redirect(w, r, "/admin/tenants/"+tid.String()+"/dids?flash=Saved.", http.StatusSeeOther)
}
