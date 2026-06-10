package portal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// errAssignExtension is shown when the supervisor has no owned extension to
// originate the spy leg to.
var errAssignExtension = errors.New("assign yourself an extension to use supervisor tools")

// Supervisor tools: monitor / whisper / barge on a live call.
//
// There is NO new data model or migration here — these are action buttons on
// the tenant's live-call view, implemented entirely by originating a call to
// the SUPERVISOR's own phone and running FreeSWITCH's mod_spy `eavesdrop`
// application against the target call's channel UUID. The supervisor's leg
// joins the existing call in one of three audio modes:
//
//   - monitor — supervisor hears both parties; neither party hears the
//     supervisor (silent monitoring).
//   - whisper — supervisor is audible to the AGENT leg only (coaching); the
//     remote/customer party still can't hear the supervisor.
//   - barge   — supervisor is audible to BOTH parties (three-way).
//
// mod_spy lets the spying party flip between these live with DTMF (1=monitor,
// 2=whisper as the call's default, 3=three-way), gated by eavesdrop_enable_dtmf.
// We pre-seed the *initial* state per mode via channel variables, set with the
// inline `&set:` app before `&eavesdrop`.

// supervisorModes is the allow-list the handler validates `mode` against.
var supervisorModes = map[string]bool{
	"monitor": true,
	"whisper": true,
	"barge":   true,
}

// buildEavesdropApp returns the FreeSWITCH application string to run on the
// supervisor's answered leg so it spies on the call whose channel UUID is
// targetUUID, in the given mode.
//
// FS eavesdrop var reference (mod_spy) — these are the best-effort initial
// states; the exact audible-side behaviour is fiddly and MUST be confirmed by
// a live-call validation pass on the box:
//
//   - eavesdrop_enable_dtmf=true  → supervisor can switch modes live with DTMF
//       (the canonical bindings inside eavesdrop are 1=mono-A, 2=mono-B,
//        3=both-channels listen; 0 toggles talk-to-both, 1/2 talk-to-one-side).
//   - monitor: no talk flags — pure listen on both legs.
//   - whisper: eavesdrop_whisper_aleg=true → supervisor talks to the A-leg
//       (the agent/bridged extension) only, so the far party can't hear them.
//       (eavesdrop_whisper_bleg=true would target the other side instead — the
//        agent-vs-customer leg orientation is exactly what live validation must
//        verify, since which leg is the "agent" depends on call direction.)
//   - barge: eavesdrop_whisper_aleg=true AND eavesdrop_whisper_bleg=true →
//       supervisor is audible to both legs (three-way conversation).
//
// The builder is deliberately pure (no I/O) so it is unit-tested for all modes.
func buildEavesdropApp(targetUUID, mode string) string {
	// Common prelude: allow live DTMF mode-switching on the supervisor's leg.
	prelude := "&set:eavesdrop_enable_dtmf=true"

	var talk string
	switch mode {
	case "whisper":
		// Audible to the agent (A-leg) only.
		talk = ",&set:eavesdrop_whisper_aleg=true"
	case "barge":
		// Audible to both parties (three-way).
		talk = ",&set:eavesdrop_whisper_aleg=true,&set:eavesdrop_whisper_bleg=true"
	default:
		// monitor — silent on both legs (no whisper vars).
		talk = ""
	}

	return prelude + talk + ",&eavesdrop:" + targetUUID
}

// liveEavesdrop handles POST /admin/tenants/{tenantID}/live/eavesdrop.
// Form: uuid (target channel) + mode (monitor|whisper|barge). Admin-only
// (the whole /live/* subtree is behind adminScopeRequired). Originates the
// supervisor's own extension and runs the eavesdrop app against the target.
func (s *Server) liveEavesdrop(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if s.originator == nil || s.live == nil {
		http.Error(w, "supervisor tools unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	targetUUID := strings.TrimSpace(r.FormValue("uuid"))
	mode := strings.TrimSpace(r.FormValue("mode"))
	if targetUUID == "" {
		http.Error(w, "missing uuid", http.StatusBadRequest)
		return
	}
	if !supervisorModes[mode] {
		http.Error(w, "invalid mode", http.StatusBadRequest)
		return
	}

	redirect := "/admin/tenants/" + tid.String() + "/live"

	// Ownership check: the target channel must belong to one of this tenant's
	// SIP domains. Re-fetch live state rather than trust the client (mirrors
	// liveHangup). The KillUUID is the call's A-leg channel UUID — a concrete
	// channel UUID, which is exactly what eavesdrop targets.
	cctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	calls, err := s.live.ActiveCalls(cctx)
	if err != nil {
		http.Error(w, "live monitoring unavailable", http.StatusServiceUnavailable)
		return
	}
	set := s.tenantDomainSet(r.Context(), tid)
	owned := false
	for _, c := range calls {
		if c.KillUUID == targetUUID && liveCallHasDomain(c, set) {
			owned = true
			break
		}
	}
	if !owned {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Resolve the supervisor's own extension (first owned, active extension).
	user := s.currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	owned2, _ := s.store.FindOwnedExtensions(r.Context(), user.ID)
	if len(owned2) == 0 {
		s.flashErr(w, r, redirect, errAssignExtension)
		return
	}
	supExt := owned2[0]

	// Resolve the domain string for the supervisor's extension by matching its
	// sip_domain_id against the tenant's SIP domains.
	supDomain := ""
	for _, d := range mustSIPDomains(r.Context(), s.store, tid) {
		if d.ID == supExt.SIPDomainID {
			supDomain = d.Domain
			break
		}
	}
	if supDomain == "" {
		s.flashErr(w, r, redirect, errAssignExtension)
		return
	}

	// Originate the supervisor's own phone via Kamailio (mirrors broadcast.go),
	// then run the eavesdrop app on answer. ignore_early_media so we wait for a
	// real answer; a SPY caller-ID makes the spy leg recognisable in CDRs.
	dial := fmt.Sprintf(
		"{origination_caller_id_number=SPY,origination_caller_id_name=Supervisor,ignore_early_media=true}sofia/internal/sip:%s@%s;fs_path=sip:%s;lr",
		supExt.SIPUsername, supDomain, s.sipRoutingTarget,
	)
	app := buildEavesdropApp(targetUUID, mode)
	if _, err := s.originator.Originate(r.Context(), dial, app); err != nil {
		s.flashErr(w, r, redirect, fmt.Errorf("could not start %s: %w", mode, err))
		return
	}

	s.auditNested(r, tid, "supervisor.eavesdrop", "call", nil, map[string]any{
		"uuid": targetUUID, "mode": mode, "supervisor_ext": supExt.Extension,
	})
	http.Redirect(w, r, redirect+"?flash="+url.QueryEscape(supervisorFlash(mode)), http.StatusSeeOther)
}

// supervisorFlash is the user-facing confirmation per mode.
func supervisorFlash(mode string) string {
	switch mode {
	case "whisper":
		return "Whisper starting — your phone is ringing. Answer to coach the agent."
	case "barge":
		return "Barge starting — your phone is ringing. Answer to join the call."
	default:
		return "Monitor starting — your phone is ringing. Answer to listen in."
	}
}
