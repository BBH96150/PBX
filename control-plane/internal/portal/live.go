package portal

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// LiveCall mirrors freeswitch.ActiveCall — duplicated here so the portal package
// doesn't import internal/freeswitch (same pattern as GatewayLiveStatus). The
// adapter in cmd/server does the field copy.
type LiveCall struct {
	CallUUID   string
	KillUUID   string
	Direction  string
	CallerNum  string
	CallerName string
	CalleeNum  string
	State      string
	StartEpoch int64
	Domains    []string
}

// LiveMonitor is the minimal interface the portal needs for the live call view.
// Backed by the FreeSWITCH ESL client. Implementations must return a non-nil
// error (e.g. ErrNotConnected) when FS is unreachable so the page can degrade.
type LiveMonitor interface {
	ActiveCalls(ctx context.Context) ([]LiveCall, error)
	Hangup(ctx context.Context, uuid string) error
}

// liveCallView is the render model: a LiveCall plus a humanized duration.
type liveCallView struct {
	LiveCall
	Dur string
}

func (s *Server) tenantDomainSet(ctx context.Context, tid uuid.UUID) map[string]struct{} {
	set := map[string]struct{}{}
	for _, d := range mustSIPDomains(ctx, s.store, tid) {
		set[strings.ToLower(d.Domain)] = struct{}{}
	}
	return set
}

func liveCallHasDomain(c LiveCall, set map[string]struct{}) bool {
	for _, d := range c.Domains {
		if _, ok := set[strings.ToLower(d)]; ok {
			return true
		}
	}
	return false
}

// scopedActiveCalls returns this tenant's in-progress calls (filtered to the
// given SIP domain set) and whether FreeSWITCH was reachable.
func (s *Server) scopedActiveCalls(ctx context.Context, set map[string]struct{}) (views []liveCallView, fsUp bool) {
	if s.live == nil {
		return nil, false
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	calls, err := s.live.ActiveCalls(cctx)
	if err != nil {
		return nil, false
	}
	now := time.Now().Unix()
	for _, c := range calls {
		if !liveCallHasDomain(c, set) {
			continue
		}
		dur := ""
		if c.StartEpoch > 0 && now > c.StartEpoch {
			dur = humandur(int(now - c.StartEpoch))
		}
		views = append(views, liveCallView{LiveCall: c, Dur: dur})
	}
	return views, true
}

// tenantPresence returns the sorted list of extension numbers currently
// registered within the tenant's SIP domains, and whether presence data was
// available (false if the location table can't be read — presence shows as
// unavailable rather than erroring the page).
func (s *Server) tenantPresence(ctx context.Context, set map[string]struct{}) (online []string, ok bool) {
	regs, err := s.store.ActiveRegistrations(ctx)
	if err != nil {
		return nil, false
	}
	seen := map[string]bool{}
	for _, r := range regs {
		if _, in := set[strings.ToLower(r.Domain)]; in && !seen[r.Username] {
			seen[r.Username] = true
			online = append(online, r.Username)
		}
	}
	sort.Strings(online)
	return online, true
}

// trunkView is the render model for one trunk's live registration state.
type trunkView struct {
	Name       string
	Carrier    string
	State      string // REGED / FAIL_WAIT / TRYING / unknown / disabled / no-register
	Registered bool
	Expected   bool // a registering, enabled trunk (counts toward the summary)
}

// tenantTrunkStatus returns each trunk's live registration state plus how many
// of the registering trunks are currently REGED. ok is false when no gateway
// syncer is wired. Each lookup degrades on its own if FS is unreachable.
func (s *Server) tenantTrunkStatus(ctx context.Context, tid uuid.UUID) (views []trunkView, regN, expectedN int, ok bool) {
	if s.gwSyncer == nil {
		return nil, 0, 0, false
	}
	accts, err := s.store.ListCarrierAccountsForTenant(ctx, tid)
	if err != nil {
		return nil, 0, 0, false
	}
	for _, a := range accts {
		tv := trunkView{Name: a.Name, Carrier: a.CarrierKind}
		switch {
		case !a.Enabled:
			tv.State = "disabled"
		case !a.Register:
			tv.State = "no-register"
		default:
			tv.Expected = true
			expectedN++
			cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			st := s.gwSyncer.GatewayStatus(cctx, a.FSGatewayName)
			cancel()
			tv.State = st.State
			if tv.State == "" {
				tv.State = "unknown"
			}
			tv.Registered = st.State == "REGED"
			if tv.Registered {
				regN++
			}
		}
		views = append(views, tv)
	}
	return views, regN, expectedN, true
}

func (s *Server) liveDashboard(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	s.renderLayout(w, r, tenant.Name+" · Live", "live", map[string]any{
		"Tenant":    tenant,
		"NavActive": "live",
	})
}

func (s *Server) liveFragment(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	set := s.tenantDomainSet(r.Context(), tid)
	calls, fsUp := s.scopedActiveCalls(r.Context(), set)
	online, presenceUp := s.tenantPresence(r.Context(), set)
	total := len(mustExtensions(r.Context(), s.store, tid))
	trunks, trunkReg, trunkExp, trunksOK := s.tenantTrunkStatus(r.Context(), tid)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpls.ExecuteTemplate(w, "live_fragment", map[string]any{
		"TenantID":   tid,
		"Calls":      calls,
		"FSUp":       fsUp,
		"HasLive":    s.live != nil,
		"Online":     online,
		"OnlineN":    len(online),
		"TotalExt":   total,
		"PresenceUp": presenceUp,
		"Trunks":     trunks,
		"TrunkReg":   trunkReg,
		"TrunkExp":   trunkExp,
		"TrunksOK":   trunksOK,
	}); err != nil {
		slog.Error("live fragment render", "err", err)
	}
}

func (s *Server) liveHangup(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if s.live == nil {
		http.Error(w, "live monitoring unavailable", http.StatusServiceUnavailable)
		return
	}
	killUUID := strings.TrimSpace(r.FormValue("uuid"))
	if killUUID == "" {
		http.Error(w, "missing uuid", http.StatusBadRequest)
		return
	}
	// Ownership check: the channel must belong to one of this tenant's SIP
	// domains. Re-fetch live state rather than trust the client.
	cctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	calls, err := s.live.ActiveCalls(cctx)
	if err != nil {
		http.Error(w, "live monitoring unavailable", http.StatusServiceUnavailable)
		return
	}
	set := s.tenantDomainSet(r.Context(), tid)
	owned := false
	var callerNum, calleeNum string
	for _, c := range calls {
		if c.KillUUID == killUUID && liveCallHasDomain(c, set) {
			owned = true
			callerNum, calleeNum = c.CallerNum, c.CalleeNum
			break
		}
	}
	if !owned {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := s.live.Hangup(cctx, killUUID); err != nil {
		slog.Error("live hangup failed", "uuid", killUUID, "err", err)
	}
	s.auditNested(r, tid, "live.call.hangup", "call", nil, map[string]any{
		"uuid": killUUID, "caller": callerNum, "callee": calleeNum,
	})
	// Return the refreshed fragment for htmx to swap in.
	s.liveFragment(w, r)
}
