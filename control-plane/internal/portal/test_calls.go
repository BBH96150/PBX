package portal

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/audit"
	"github.com/tendpos/sip-platform/control-plane/internal/e164"
)

// trunkTestOutbound dials a real PSTN number THROUGH the trunk, plays a
// short announcement when the destination answers, then hangs up. Useful to
// verify "trunk auth + outbound bridge + CallerID-from-trunk" all work
// without a softphone in the picture.
func (s *Server) trunkTestOutbound(w http.ResponseWriter, r *http.Request) {
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
	dest := strings.TrimSpace(r.FormValue("dest"))
	normalized, nerr := e164.Normalize(dest, "US")
	if nerr != nil {
		s.testCallFragment(w, "Bad destination — use a real phone number like +14155551234 or 415-555-1234.", "", true)
		return
	}
	acct, err := s.store.GetCarrierAccountForTenant(r.Context(), tid, aid)
	if err != nil {
		s.testCallFragment(w, err.Error(), "", true)
		return
	}
	if s.originator == nil {
		s.testCallFragment(w, "ESL not connected — can't originate test call.", "", true)
		return
	}
	dialDigits := e164.DialDigits(normalized)
	// Use the trunk's SIP username as the From URI identity so it matches
	// the authenticated REGISTER identity. CallCentric (and most carriers)
	// will intercept calls whose From URI doesn't match the auth user with
	// an instant 200 OK + recorded message. The MainDID is what the called
	// party sees as Caller-ID (effective_caller_id_*); the SIP-level From
	// identity is separate.
	cid := acct.SIPUsername
	displayCID := e164.DialDigits(acct.MainDIDE164)
	if displayCID == "" {
		displayCID = acct.SIPUsername
	}
	// Originate variables: caller-id from trunk, mark as test so CDR shows it.
	//
	// `send_silence_when_idle=400` makes FS emit comfort-noise RTP packets
	// continuously even when there's no real audio to send. That's critical
	// behind home/SMB NAT: without it, echo (or any "receive then emit" app)
	// sits silent waiting for input — but the home router has no inbound
	// pinhole for the call's RTP port until WE send outbound first.
	// Continuous comfort noise keeps the NAT mapping open in both directions.
	//
	// `bypass_media=false` ensures FS terminates RTP locally rather than
	// asking the carrier to do peer-to-peer media (which CallCentric won't).
	vars := []string{
		"origination_caller_id_number=" + cid,
		"origination_caller_id_name='SIP Platform Test'",
		"effective_caller_id_number=" + displayCID,
		"effective_caller_id_name='SIP Platform Test'",
		"sip_from_uri=sip:" + cid + "@" + firstNonEmpty(acct.AuthRealm, acct.CarrierProxyHost, "callcentric.com"),
		"send_silence_when_idle=400",
		"bypass_media=false",
		"x_call_direction=outbound",
		"x_test_call=true",
		"x_carrier_account_id=" + acct.ID.String(),
		"hangup_after_bridge=true",
		"ignore_early_media=true",
	}
	dialString := "{" + strings.Join(vars, ",") + "}sofia/gateway/" + acct.FSGatewayName + "/" + dialDigits
	// When answered, run echo so the user can verify two-way audio
	// (speak → hear themselves ~1s later). Stays alive until they hang up.
	app := "&echo"

	out, err := s.originator.Originate(r.Context(), dialString, app)
	if err != nil {
		s.testCallFragment(w, "originate failed: "+err.Error(), "", true)
		return
	}
	out = strings.TrimSpace(out)
	ok2 := strings.HasPrefix(out, "+OK")
	var msg string
	callUUID := ""
	switch {
	case ok2:
		// bgapi reply format: "+OK Job-UUID: <uuid>"
		rest := strings.TrimSpace(strings.TrimPrefix(out, "+OK"))
		if i := strings.Index(rest, "Job-UUID:"); i >= 0 {
			callUUID = strings.TrimSpace(rest[i+len("Job-UUID:"):])
		} else {
			callUUID = rest
		}
		msg = "Call placed through " + acct.Name + " to " + normalized + ". Your phone should ring shortly — answer and SPEAK; you'll hear yourself echoed back ~1 second later. Hang up when done."
	case strings.Contains(out, "i/o timeout"), strings.Contains(out, "timeout"):
		msg = "FreeSWITCH didn't respond in time. Try again — if it keeps happening, the ESL socket may be wedged and the control plane needs a restart."
	case strings.Contains(out, "GATEWAY_DOWN"), strings.Contains(out, "INVALID_GATEWAY"):
		msg = "Trunk isn't registered with the carrier right now — check the live status pill in the trunk row."
	case strings.Contains(out, "CALL_REJECTED"):
		msg = "Carrier rejected the call (CALL_REJECTED). Common causes: destination number not allowed by your CallCentric plan, account credit exhausted, or international call to a blocked region. Try your own cell phone first."
	case strings.Contains(out, "NORMAL_TEMPORARY_FAILURE"), strings.Contains(out, "RECOVERY_ON_TIMER_EXPIRE"):
		msg = "Carrier didn't answer in time. The trunk reached CallCentric but they timed out."
	case strings.Contains(out, "UNALLOCATED_NUMBER"):
		msg = "Number " + normalized + " doesn't exist according to CallCentric."
	default:
		msg = "Originate result: " + out
	}

	ip, ua := audit.FromRequest(r)
	var actorTok *uuid.UUID
	if tok := tokenFromCtx(r.Context()); tok != nil {
		actorTok = &tok.ID
	}
	s.audit.Log(r.Context(), audit.Event{
		TenantID: &tid, ActorTokenID: actorTok,
		Event: "trunk.test_call.outbound",
		TargetType: "carrier_account", TargetID: &acct.ID,
		IPAddress: ip, UserAgent: ua,
		Payload: map[string]any{"dest": normalized, "call_uuid": callUUID, "result": out},
	})
	s.testCallFragment(w, msg, callUUID, !ok2)
}

// didTestInbound bounces a call through the public context as if it had
// arrived from the carrier, so the user can verify DID routing to the
// destination extension/IVR/etc. without dialing a real phone number.
func (s *Server) didTestInbound(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	didID, err := uuid.Parse(chi.URLParam(r, "didID"))
	if err != nil {
		http.Error(w, "bad did id", 400)
		return
	}
	dids, _ := s.store.ListDIDsForTenant(r.Context(), tid)
	var target *string
	var didE164 string
	for _, d := range dids {
		if d.ID == didID {
			didE164 = d.E164
			break
		}
	}
	if didE164 == "" {
		s.testCallFragment(w, "DID not found.", "", true)
		return
	}
	_ = target

	if s.originator == nil {
		s.testCallFragment(w, "ESL not connected — can't originate test call.", "", true)
		return
	}

	// loopback channel format: loopback/<destination>/<dialplan>/<context>.
	// "XML/public" routes destination through our handleInboundPSTN handler
	// (xml_curl serves dialplan for the public context), which then bridges
	// to the destination extension.
	digits := e164.DialDigits(didE164)
	vars := []string{
		"origination_caller_id_number=15555550100",
		"origination_caller_id_name='Inbound Test'",
		"x_test_call=true",
		"x_inbound_test=true",
		"hangup_after_bridge=true",
	}
	dialString := "{" + strings.Join(vars, ",") + "}loopback/" + digits + "/XML/public"
	app := "&park"

	out, err := s.originator.Originate(r.Context(), dialString, app)
	if err != nil {
		s.testCallFragment(w, "originate failed: "+err.Error(), "", true)
		return
	}
	out = strings.TrimSpace(out)
	ok2 := strings.HasPrefix(out, "+OK")
	var msg string
	callUUID := ""
	switch {
	case ok2:
		rest := strings.TrimSpace(strings.TrimPrefix(out, "+OK"))
		if i := strings.Index(rest, "Job-UUID:"); i >= 0 {
			callUUID = strings.TrimSpace(rest[i+len("Job-UUID:"):])
		} else {
			callUUID = rest
		}
		msg = "Simulated inbound call to " + didE164 + "… target extension should ring."
	case strings.Contains(out, "NO_ROUTE_DESTINATION"), strings.Contains(out, "NO_USER_RESPONSE"):
		msg = "DID routing is correct, but no device is currently registered to the target extension. Register a softphone as that extension and try again."
	case strings.Contains(out, "UNALLOCATED_NUMBER"):
		msg = "FreeSWITCH says no DID is configured for " + didE164 + " — check the DB row is enabled."
	default:
		msg = "Originate result: " + out
	}

	ip, ua := audit.FromRequest(r)
	var actorTok *uuid.UUID
	if tok := tokenFromCtx(r.Context()); tok != nil {
		actorTok = &tok.ID
	}
	s.audit.Log(r.Context(), audit.Event{
		TenantID: &tid, ActorTokenID: actorTok,
		Event: "did.test_call.inbound",
		TargetType: "did", TargetID: &didID,
		IPAddress: ip, UserAgent: ua,
		Payload: map[string]any{"did": didE164, "call_uuid": callUUID, "result": out},
	})
	s.testCallFragment(w, msg, callUUID, !ok2)
}

// testCallFragment renders a tiny HTML banner returned to htmx swaps.
func (s *Server) testCallFragment(w http.ResponseWriter, msg, callUUID string, isErr bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cls := "pill pill-ok"
	if isErr {
		cls = "pill pill-warn"
	}
	body := `<span class="` + cls + `">` + escapeHTML(msg) + `</span>`
	if callUUID != "" {
		body += ` <code style="font-size:0.78rem;margin-left:0.35rem">` + escapeHTML(callUUID) + `</code>`
	}
	_, _ = w.Write([]byte(body))
}

func escapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return r.Replace(s)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

var _ = errors.New // keep imports tidy
