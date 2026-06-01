package portal

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/audit"
	"github.com/tendpos/sip-platform/control-plane/internal/e164"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// outboundRoutesList shows the per-tenant dial-prefix → trunk routing table.
func (s *Server) outboundRoutesList(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	routes, _ := s.store.ListOutboundRoutesForTenant(r.Context(), tid)
	accounts, _ := s.store.ListCarrierAccountsForTenant(r.Context(), tid)

	// Trunk-id → label so rows show the trunk name, not a UUID.
	acctLabel := map[uuid.UUID]string{}
	for _, a := range accounts {
		acctLabel[a.ID] = a.Name
	}

	s.renderLayout(w, r, tenant.Name+" · Outbound routing", "outbound_routes", map[string]any{
		"Tenant":    tenant,
		"Routes":    routes,
		"Accounts":  accounts,
		"AcctLabel": acctLabel,
	})
}

func (s *Server) outboundRouteCreate(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/outbound-routes"

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		s.flashErr(w, r, redirect, errors.New("give the route a name"))
		return
	}

	// match_prefix is optional; '' = catch-all. If present it must be a clean
	// E.164 prefix (leading +, digits only) — match the DB check constraint.
	prefix := strings.TrimSpace(r.FormValue("match_prefix"))
	if prefix != "" && !validE164Prefix(prefix) {
		s.flashErr(w, r, redirect,
			errors.New("prefix must be a leading + and digits, e.g. +1 or +1415 (or blank for catch-all)"))
		return
	}

	acctID, err := uuid.Parse(r.FormValue("carrier_account_id"))
	if err != nil {
		s.flashErr(w, r, redirect, errors.New("pick which trunk carries these calls"))
		return
	}
	if _, err := s.store.GetCarrierAccountForTenant(r.Context(), tid, acctID); err != nil {
		s.flashErr(w, r, redirect, errors.New("that trunk is no longer available"))
		return
	}

	// Optional caller-ID override. Normalize to E.164 so it matches the
	// constraint and presents cleanly.
	var cid string
	if raw := strings.TrimSpace(r.FormValue("caller_id_e164")); raw != "" {
		norm, nerr := e164.Normalize(raw, "US")
		if nerr != nil {
			s.flashErr(w, r, redirect, errors.New("caller ID must be E.164 like +14155551234"))
			return
		}
		if !strings.HasPrefix(norm, "+") {
			norm = "+" + norm
		}
		cid = norm
	}

	priority := 100
	if p, perr := strconv.Atoi(strings.TrimSpace(r.FormValue("priority"))); perr == nil {
		priority = p
	}

	route, err := s.store.CreateOutboundRoute(r.Context(), store.CreateOutboundRouteInput{
		TenantID:         tid,
		Name:             name,
		MatchPrefix:      prefix,
		CarrierAccountID: acctID,
		CallerIDE164:     cid,
		Priority:         priority,
	})
	if err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}

	ip, ua := audit.FromRequest(r)
	var actorTok *uuid.UUID
	if tok := tokenFromCtx(r.Context()); tok != nil {
		actorTok = &tok.ID
	}
	s.audit.Log(r.Context(), audit.Event{
		TenantID: &tid, ActorTokenID: actorTok,
		Event: "outbound_route.created", TargetType: "outbound_route", TargetID: &route.ID,
		IPAddress: ip, UserAgent: ua,
		Payload: map[string]any{"name": name, "match_prefix": prefix, "carrier_account_id": acctID.String()},
	})
	http.Redirect(w, r, redirect+"?flash=Route+added.", http.StatusSeeOther)
}

func (s *Server) outboundRouteDelete(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "routeID"))
	if err != nil {
		http.Error(w, "bad route id", 400)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/outbound-routes"
	if err := s.store.DeleteOutboundRouteForTenant(r.Context(), tid, id); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	ip, ua := audit.FromRequest(r)
	var actorTok *uuid.UUID
	if tok := tokenFromCtx(r.Context()); tok != nil {
		actorTok = &tok.ID
	}
	s.audit.Log(r.Context(), audit.Event{
		TenantID: &tid, ActorTokenID: actorTok,
		Event: "outbound_route.deleted", TargetType: "outbound_route", TargetID: &id,
		IPAddress: ip, UserAgent: ua,
	})
	http.Redirect(w, r, redirect+"?flash=Route+removed.", http.StatusSeeOther)
}

func (s *Server) outboundRouteToggle(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "routeID"))
	if err != nil {
		http.Error(w, "bad route id", 400)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/outbound-routes"
	enabled := r.FormValue("enabled") == "true"
	if err := s.store.SetOutboundRouteEnabledForTenant(r.Context(), tid, id, enabled); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	http.Redirect(w, r, redirect+"?flash=Saved.", http.StatusSeeOther)
}

// validE164Prefix reports whether p is a leading + followed by one or more
// digits — the same shape the DB CHECK constraint enforces.
func validE164Prefix(p string) bool {
	if len(p) < 2 || p[0] != '+' {
		return false
	}
	for _, c := range p[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
