package portal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/audit"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// GatewaySyncer is the minimal interface the portal needs from
// internal/freeswitch.GatewayProvisioner. Defined here so the portal
// package doesn't import the freeswitch one.
type GatewaySyncer interface {
	SyncGateways(ctx context.Context) error
	GatewayStatus(ctx context.Context, gatewayName string) GatewayLiveStatus
}

// CallOriginator is what the portal uses to fire test calls. Implemented by
// internal/freeswitch.ESLClient via main.go's adapter.
type CallOriginator interface {
	Originate(ctx context.Context, dialString, application string) (string, error)
}

// GatewayLiveStatus mirrors freeswitch.GatewayLiveStatus — duplicated here
// so the portal package doesn't need to import the freeswitch one.
// Field names + tags must match for the adapter shim in main.go to work.
type GatewayLiveStatus struct {
	Found       bool
	State       string
	Status      string
	PingTime    string
	Uptime      string
	CallsIn     string
	CallsOut    string
	Error       string
	LastSIPCode string
	LastSIPMsg  string
}

func (s *Server) trunksList(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	accounts, _ := s.store.ListCarrierAccountsForTenant(r.Context(), tid)
	carriers, _ := s.store.ListCarriers(r.Context())

	// Count the DIDs routed through each trunk so the delete button can warn.
	dids, _ := s.store.ListDIDsForTenant(r.Context(), tid)
	didCountByAccount := map[uuid.UUID]int{}
	for _, d := range dids {
		if d.CarrierAccountID != nil {
			didCountByAccount[*d.CarrierAccountID]++
		}
	}

	// renderLayout pulls ?flash=...&err=... out of the URL automatically;
	// don't override or we lose the error path.
	s.renderLayout(w, r, tenant.Name+" · Phone trunks", "trunks", map[string]any{
		"Tenant":            tenant,
		"Accounts":          accounts,
		"Carriers":          carriers,
		"DIDCountByAccount": didCountByAccount,
	})
}

func (s *Server) trunkCreate(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	kind := strings.TrimSpace(r.FormValue("carrier_kind"))
	name := strings.TrimSpace(r.FormValue("name"))
	sipUser := strings.TrimSpace(r.FormValue("sip_username"))
	sipPass := r.FormValue("sip_password")
	mainDID := strings.TrimSpace(r.FormValue("main_did_e164"))

	if kind == "" || name == "" || sipUser == "" || sipPass == "" {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/trunks",
			httpErr("carrier, name, sip username, sip password are required"))
		return
	}
	carrier, err := s.store.GetCarrierByKind(r.Context(), kind)
	if err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/trunks",
			httpErr("unknown carrier kind "+kind))
		return
	}
	// fs_gateway_name has a platform-wide UNIQUE constraint, so we mix in
	// a short random suffix. That way deleting + re-adding the same
	// (carrier, sip_username) doesn't collide on the second save.
	gwName := fsGatewayName(kind, tid, sipUser)
	tidCopy := tid
	port := 0
	if v := strings.TrimSpace(r.FormValue("proxy_port_override")); v != "" {
		port, _ = strconv.Atoi(v)
	}
	acct, err := s.store.CreateCarrierAccount(r.Context(), store.CreateCarrierAccountInput{
		TenantID:          &tidCopy,
		CarrierID:         carrier.ID,
		Name:              name,
		SIPUsername:       sipUser,
		SIPPassword:       sipPass,
		AuthRealm:         strings.TrimSpace(r.FormValue("auth_realm")),
		ProxyHostOverride: strings.TrimSpace(r.FormValue("proxy_host_override")),
		ProxyPortOverride: port,
		TransportOverride: normTransport(r.FormValue("transport_override")),
		FSGatewayName:     gwName,
		Register:          r.FormValue("register") != "false",
		MainDIDE164:       mainDID,
	})
	if err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/trunks", friendlyCarrierErr(err))
		return
	}
	s.syncAndAudit(r, &tid, &acct.ID, "carrier.account.created", map[string]any{
		"carrier": kind, "name": name, "sip_username": sipUser,
	})
	http.Redirect(w, r,
		"/admin/tenants/"+tid.String()+"/trunks?flash=Trunk+saved.+Watch+the+Live+registration+column+%E2%80%94+it+updates+every+2+seconds.",
		http.StatusSeeOther)
}

func (s *Server) trunkUpdate(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	aid, err := uuid.Parse(chi.URLParam(r, "accountID"))
	if err != nil {
		http.Error(w, "bad account id", 400)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	port := 0
	if v := strings.TrimSpace(r.FormValue("proxy_port_override")); v != "" {
		port, _ = strconv.Atoi(v)
	}
	in := store.UpdateCarrierAccountInput{
		Name:              strings.TrimSpace(r.FormValue("name")),
		SIPUsername:       strings.TrimSpace(r.FormValue("sip_username")),
		SIPPassword:       r.FormValue("sip_password"), // empty → keep
		AuthRealm:         strings.TrimSpace(r.FormValue("auth_realm")),
		ProxyHostOverride: strings.TrimSpace(r.FormValue("proxy_host_override")),
		ProxyPortOverride: port,
		TransportOverride: normTransport(r.FormValue("transport_override")),
		Register:          r.FormValue("register") != "false",
		MainDIDE164:       strings.TrimSpace(r.FormValue("main_did_e164")),
		Enabled:           r.FormValue("enabled") != "false",
	}
	if err := s.store.UpdateCarrierAccountForTenant(r.Context(), tid, aid, in); err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/trunks", friendlyCarrierErr(err))
		return
	}
	s.syncAndAudit(r, &tid, &aid, "carrier.account.updated", map[string]any{
		"name": in.Name, "enabled": in.Enabled, "secret_changed": in.SIPPassword != "",
	})
	http.Redirect(w, r, "/admin/tenants/"+tid.String()+"/trunks?flash=Trunk+updated.", http.StatusSeeOther)
}

// trunkStatusFragment is the HTMX poll target — returns a small HTML block
// (badge + ancillary stats) that the trunks page swaps in every couple of
// seconds.
func (s *Server) trunkStatusFragment(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	aid, err := uuid.Parse(chi.URLParam(r, "accountID"))
	if err != nil {
		http.Error(w, "bad account id", 400)
		return
	}
	acct, err := s.store.GetCarrierAccountForTenant(r.Context(), tid, aid)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	var st GatewayLiveStatus
	if s.gwSyncer != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		st = s.gwSyncer.GatewayStatus(ctx, acct.FSGatewayName)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpls.ExecuteTemplate(w, "trunk_status_fragment", map[string]any{
		"Status":  st,
		"Account": acct,
	}); err != nil {
		slog.Error("trunk status fragment render", "err", err)
	}
}

func (s *Server) trunkDelete(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	aid, err := uuid.Parse(chi.URLParam(r, "accountID"))
	if err != nil {
		http.Error(w, "bad account id", 400)
		return
	}
	cascadedDIDs, err := s.store.DeleteCarrierAccountForTenant(r.Context(), tid, aid)
	if err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/trunks", err)
		return
	}
	s.syncAndAudit(r, &tid, &aid, "carrier.account.deleted", map[string]any{
		"cascaded_dids": cascadedDIDs,
	})
	flash := "Trunk+removed."
	if cascadedDIDs > 0 {
		flash = fmt.Sprintf("Trunk+removed+(also+removed+%d+routed+number(s)).", cascadedDIDs)
	}
	http.Redirect(w, r, "/admin/tenants/"+tid.String()+"/trunks?flash="+flash, http.StatusSeeOther)
}

func (s *Server) syncAndAudit(r *http.Request, tid *uuid.UUID, targetID *uuid.UUID, event string, payload map[string]any) {
	if s.gwSyncer != nil {
		if err := s.gwSyncer.SyncGateways(r.Context()); err != nil {
			// Don't fail the user-facing action — gateway sync is best-effort.
			// Worst case, admin re-saves to retry.
			s.audit.Log(r.Context(), audit.Event{
				TenantID: tid, Event: "carrier.gateway.sync_failed",
				TargetType: "carrier_account", TargetID: targetID,
				Payload: map[string]any{"err": err.Error()},
			})
		}
	}
	ip, ua := audit.FromRequest(r)
	var actorTok *uuid.UUID
	if tok := tokenFromCtx(r.Context()); tok != nil {
		actorTok = &tok.ID
	}
	s.audit.Log(r.Context(), audit.Event{
		TenantID: tid, ActorTokenID: actorTok,
		Event: event, TargetType: "carrier_account", TargetID: targetID,
		IPAddress: ip, UserAgent: ua,
		Payload: payload,
	})
}

// fsGatewayName composes a unique-platform-wide gateway name from the
// tenant + carrier + sip user + a short random suffix. The DB check
// constraint requires lowercase ASCII letters, digits, and underscores
// only (no dashes). The suffix means deleting a trunk and adding a fresh
// one with the same SIP username generates a different gateway name —
// no unique-constraint collision on the second save.
func fsGatewayName(kind string, tenantID uuid.UUID, sipUser string) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(kind))
	b.WriteByte('_')
	// First 8 hex chars of the tenant UUID — uuid.String() lowercases already.
	b.WriteString(strings.ReplaceAll(tenantID.String()[:8], "-", "_"))
	b.WriteByte('_')
	for _, r := range strings.ToLower(sipUser) {
		switch {
		case r >= 'a' && r <= 'z',
			r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	b.WriteByte('_')
	b.WriteString(randomHexSuffix(6))
	return b.String()
}

// randomHexSuffix returns n lowercase hex chars. Used to disambiguate
// gateway names within the same (tenant, carrier, sip_user) so legitimate
// "delete and re-create" works.
func randomHexSuffix(n int) string {
	b := make([]byte, (n+1)/2)
	if _, err := rand.Read(b); err != nil {
		// Fall back to time-based pseudo-randomness if /dev/urandom fails;
		// this is for naming uniqueness only, not security.
		return fmt.Sprintf("%06x", time.Now().UnixNano()&0xffffff)
	}
	return hex.EncodeToString(b)[:n]
}

type stringErr string

func (s stringErr) Error() string { return string(s) }

func httpErr(s string) error { return stringErr(s) }

// friendlyCarrierErr translates Postgres unique/check-constraint errors
// from the carrier_accounts table into human-readable messages so the
// admin sees "A trunk with these settings already exists" instead of a
// raw "SQLSTATE 23505" diagnostic in the red banner.
func friendlyCarrierErr(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "carrier_accounts_fs_gateway_name_key"):
		return httpErr("A trunk with this carrier + SIP username already exists in another workspace. Try a slightly different SIP username, or delete the older trunk first.")
	case strings.Contains(msg, "carrier_accounts_carrier_id_sip_username_key"):
		return httpErr("A trunk with this carrier and SIP username already exists for this workspace.")
	case strings.Contains(msg, "carrier_accounts_fs_gateway_name_check"):
		return httpErr("Internal: generated FreeSWITCH gateway name is invalid. This is a platform bug — paste this into a support ticket.")
	case strings.Contains(msg, "violates check constraint"):
		return httpErr("One of the fields didn't pass validation. Check that all values are sensible (port 1-65535, transport udp/tcp/tls, etc.).")
	}
	return err
}

// normTransport accepts "udp"/"tcp"/"tls"/"" — anything else becomes "" so
// the gateway template falls back to the carrier's default.
func normTransport(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "udp", "tcp", "tls":
		return strings.ToLower(strings.TrimSpace(s))
	}
	return ""
}
