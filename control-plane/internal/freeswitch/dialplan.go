package freeswitch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"text/template"

	"github.com/jackc/pgx/v5"

	"github.com/tendpos/sip-platform/control-plane/internal/e164"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// Handler responds to FreeSWITCH mod_xml_curl dialplan lookups.
//
// Phase 2 routing matrix:
//   context=default + destination is internal extension → bridge via Kamailio
//   context=default + destination looks like E.164      → outbound PSTN via gateway
//   context=public  (carrier gateways' inbound context) → DID lookup → inbound to extension
type Handler struct {
	store     *store.Store
	sipTarget string // host:port of Kamailio
}

func NewHandler(s *store.Store, kamailioSIPTarget string) *Handler {
	return &Handler{store: s, sipTarget: kamailioSIPTarget}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeNotFound(w)
		return
	}
	if r.FormValue("section") != "dialplan" {
		writeNotFound(w)
		return
	}

	destNum := firstNonEmpty(
		r.FormValue("destination_number"),
		r.FormValue("Caller-Destination-Number"),
		r.FormValue("Hunt-Destination-Number"),
	)
	if destNum == "" {
		writeNotFound(w)
		return
	}

	context := firstNonEmpty(
		r.FormValue("Hunt-Context"),
		r.FormValue("Caller-Context"),
		r.FormValue("context"),
		"default",
	)
	tenantDomain := firstNonEmpty(
		r.FormValue("variable_sip_h_X-Sip-Tenant-Domain"),
		r.FormValue("variable_sip_h_x-sip-tenant-domain"),
		r.FormValue("Hunt-Domain"),
		r.FormValue("variable_domain_name"),
	)

	slog.Info("dialplan lookup",
		"context", context, "dest", destNum, "tenant_domain", tenantDomain,
		"caller", r.FormValue("Caller-Caller-ID-Number"),
	)

	switch context {
	case "public":
		h.handleInboundPSTN(w, r, destNum)
	default:
		h.handleDefault(w, r, destNum, tenantDomain)
	}
}

// handleDefault is the internal-or-outbound branch: extension if it looks
// like an internal number; PSTN gateway if it looks external.
func (h *Handler) handleDefault(w http.ResponseWriter, r *http.Request, destNum, tenantDomain string) {
	// Echo/info shortcuts are served from the static dialplan.
	if destNum == "9999" || destNum == "9001" {
		writeNotFound(w)
		return
	}

	// Phase 3 Wave 2: *97 = check own voicemail.
	if destNum == "*97" {
		h.handleVoicemailCheck(w, r, tenantDomain)
		return
	}

	if e164.LooksLikeExternal(destNum) {
		h.handleOutboundPSTN(w, r, destNum, tenantDomain)
		return
	}

	// Internal extension routing (Phase 1 behavior).
	ctx := r.Context()
	var (
		ext    *store.Extension
		domain string
		err    error
	)
	if tenantDomain != "" {
		ext, domain, err = h.store.LookupExtensionForRouting(ctx, tenantDomain, destNum)
	} else {
		ext, domain, err = h.store.LookupExtensionByNumberOnly(ctx, destNum)
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Phase 3: try ring groups, IVRs, queues. Lookups need tenant_domain.
			if tenantDomain != "" {
				if info, rgErr := h.store.LookupRingGroupByExtension(ctx, tenantDomain, destNum); rgErr == nil {
					h.routeRingGroup(ctx, w, info, destNum, "default")
					return
				}
				if ivr, ivrErr := h.store.LookupIVRByExtension(ctx, tenantDomain, destNum); ivrErr == nil {
					h.routeIVR(w, ivr, destNum, "default")
					return
				}
				if q, qErr := h.store.LookupQueueByExtension(ctx, tenantDomain, destNum); qErr == nil {
					h.routeQueue(w, q, destNum, "default")
					return
				}
			}
			writeHangup(w, "default", "NO_USER_RESPONSE")
			return
		}
		slog.Error("extension lookup failed", "err", err)
		writeHangup(w, "default", "TEMPORARY_FAILURE")
		return
	}

	// Phase 3 Wave 5.0: DND short-circuits the bridge.
	if ext.DoNotDisturb {
		if ext.VoicemailEnabled {
			writeDialplan(w, dialplanData{
				Context: "default",
				Name:    "dnd-vm-" + destNum,
				Actions: []dialplanAction{
					{App: "set", Data: "x_call_direction=internal"},
					{App: "set", Data: "x_tenant_id=" + ext.TenantID.String()},
					{App: "set", Data: "x_extension_id=" + ext.ID.String()},
					{App: "set", Data: "x_dnd=true"},
					{App: "answer"},
					{App: "sleep", Data: "500"},
					{App: "voicemail", Data: fmt.Sprintf("default %s %s", domain, ext.SIPUsername)},
				},
			})
			return
		}
		writeHangup(w, "default", "USER_BUSY")
		return
	}

	// Phase 3 Wave 5.0: CF immediate rewrites the destination via transfer.
	// Caller re-enters dialplan with the new number → existing routing applies.
	if ext.CFImmediate != "" {
		writeDialplan(w, dialplanData{
			Context: "default",
			Name:    "cf-immediate-" + destNum,
			Actions: []dialplanAction{
				{App: "set", Data: "x_call_direction=internal"},
				{App: "set", Data: "x_tenant_id=" + ext.TenantID.String()},
				{App: "set", Data: "x_cf_origin_extension_id=" + ext.ID.String()},
				{App: "transfer", Data: ext.CFImmediate + " XML default"},
			},
		})
		return
	}

	bridgeURI := fmt.Sprintf("sofia/internal/sip:%s@%s;fs_path=sip:%s;lr",
		ext.SIPUsername, domain, h.sipTarget)

	actions := []dialplanAction{
		{App: "set", Data: "hangup_after_bridge=true"},
		{App: "set", Data: "continue_on_fail=true"},
		{App: "set", Data: "x_call_direction=internal"},
		{App: "set", Data: "x_tenant_id=" + ext.TenantID.String()},
		{App: "set", Data: "x_extension_id=" + ext.ID.String()},
		// Phase 3 Wave 5.5: feature codes — caller can press *2 / *3 mid-call.
		{App: "bind_meta_app", Data: "2 a s execute_extension::att_xfer XML features"},
		{App: "bind_meta_app", Data: "3 a s execute_extension::blind_xfer XML features"},
	}

	// Phase 3 Wave 5.0: always-record. record_session writes to a per-tenant /
	// per-day path; the channel var ${recording_path} persists into the hangup
	// event so the CDR pipeline picks it up.
	if ext.RecordingEnabled {
		recPath := fmt.Sprintf("$${recordings_dir}/%s/${strftime(%%Y-%%m-%%d)}/${uuid}.wav",
			ext.TenantID.String())
		actions = append(actions,
			dialplanAction{App: "set", Data: "recording_path=" + recPath},
			dialplanAction{App: "record_session", Data: "${recording_path}"},
		)
	}

	actions = append(actions, dialplanAction{App: "bridge", Data: bridgeURI})

	// Phase 3 Wave 5.0–5.5: post-bridge fallback chain.
	//
	//   cf_busy + cf_no_answer set → branch on originate_disposition via ${cond}
	//   only cf_busy               → transfer only when USER_BUSY; else voicemail (if enabled)
	//   only cf_no_answer          → unconditional transfer
	//   no CF, voicemail enabled   → leave-message fallback
	actions = append(actions, buildCFFallbackActions(ext, domain)...)

	writeDialplan(w, dialplanData{
		Context: "default",
		Name:    "internal-" + destNum,
		Actions: actions,
	})
}

// handleOutboundPSTN normalizes the destination, picks the calling tenant's
// primary carrier_account (Phase 5.1), and bridges through the corresponding
// Sofia gateway. Falls back to a platform-wide pick for legacy callers that
// don't carry a tenant domain header.
func (h *Handler) handleOutboundPSTN(w http.ResponseWriter, r *http.Request, destNum, tenantDomain string) {
	normalized, err := e164.Normalize(destNum, "US")
	if err != nil {
		slog.Info("outbound number not normalizable", "dest", destNum, "err", err)
		writeHangup(w, "default", "UNALLOCATED_NUMBER")
		return
	}

	var acct *store.CarrierAccount
	var cidOverride string
	if tenantDomain != "" {
		if tenant, terr := h.store.GetTenantBySIPDomain(r.Context(), tenantDomain); terr == nil && tenant != nil {
			// 1. Explicit per-tenant outbound route (longest-prefix match).
			if dec, rerr := h.store.ResolveOutboundRouteForTenant(r.Context(), tenant.ID, normalized); rerr == nil {
				acct = &dec.Account
				cidOverride = dec.CIDOverride
			} else if !errors.Is(rerr, pgx.ErrNoRows) {
				slog.Warn("outbound route lookup failed; will fall back", "tenant", tenant.ID, "err", rerr)
			}
			// 2. No route configured → legacy primary-carrier pick for the tenant.
			if acct == nil {
				if a, perr := h.store.PickPrimaryCarrierAccountForTenant(r.Context(), tenant.ID); perr == nil {
					acct = a
				} else if !errors.Is(perr, pgx.ErrNoRows) {
					slog.Warn("tenant carrier lookup failed; will fall back", "tenant", tenant.ID, "err", perr)
				}
			}
		}
	}
	if acct == nil {
		a, err := h.store.PickPrimaryCarrierAccount(r.Context())
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				slog.Warn("no enabled carrier_account for outbound", "dest", normalized, "tenant_domain", tenantDomain)
				writeHangup(w, "default", "NO_ROUTE_DESTINATION")
				return
			}
			slog.Error("carrier lookup failed", "err", err)
			writeHangup(w, "default", "TEMPORARY_FAILURE")
			return
		}
		acct = a
	}

	dialDigits := e164.DialDigits(normalized)
	bridgeURI := "sofia/gateway/" + acct.FSGatewayName + "/" + dialDigits

	cid := e164.DialDigits(acct.MainDIDE164)
	if cidOverride != "" {
		cid = e164.DialDigits(cidOverride)
	}
	cidName := acct.Name

	actions := []dialplanAction{
		{App: "set", Data: "hangup_after_bridge=true"},
		{App: "set", Data: "x_call_direction=outbound"},
		{App: "set", Data: "x_carrier_account_id=" + acct.ID.String()},
		{App: "set", Data: "x_dialed_e164=" + normalized},
	}
	if cid != "" {
		actions = append(actions,
			dialplanAction{App: "set", Data: "effective_caller_id_number=" + cid},
		)
	}
	if cidName != "" {
		actions = append(actions,
			dialplanAction{App: "set", Data: "effective_caller_id_name=" + cidName},
		)
	}
	actions = append(actions, dialplanAction{App: "bridge", Data: bridgeURI})

	writeDialplan(w, dialplanData{
		Context: "default",
		Name:    "outbound-" + dialDigits,
		Actions: actions,
	})
}

// handleInboundPSTN serves the dialplan for calls arriving on the external
// profile from a carrier gateway. destNum is the URI user-part FreeSWITCH
// resolved as the destination, which is often the dialed DID — but for
// carriers like CallCentric that route inbound to our gateway's Contact
// URI ("gw+<gateway>@<our-ip>"), Sofia sets destination_number to the
// gateway login (e.g. 17778718016150) and the real DID is in the To:
// header. We try destNum first, then fall back to variable_sip_to_user.
func (h *Handler) handleInboundPSTN(w http.ResponseWriter, r *http.Request, destNum string) {
	normalized, err := e164.Normalize(destNum, "US")
	if err != nil {
		// Fallback: the destination_number wasn't a valid E.164. Many SIP
		// trunks (CallCentric, some Bandwidth setups, etc.) put the dialed
		// DID in the To: URI user-part instead of the Request-URI. Try that.
		if toUser := r.FormValue("variable_sip_to_user"); toUser != "" && toUser != destNum {
			if n, terr := e164.Normalize(toUser, "US"); terr == nil {
				slog.Info("inbound DID via To: header fallback",
					"dest_orig", destNum, "to_user", toUser, "normalized", n)
				normalized = n
				err = nil
			}
		}
	}
	if err != nil {
		slog.Info("inbound DID not normalizable",
			"dest", destNum, "to_user", r.FormValue("variable_sip_to_user"), "err", err)
		writeHangup(w, "public", "UNALLOCATED_NUMBER")
		return
	}

	target, err := h.store.LookupDIDExtensionTarget(r.Context(), normalized)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Phase 3 Wave 1: ring group destination.
			if info, rgErr := h.store.LookupDIDRingGroupTarget(r.Context(), normalized); rgErr == nil {
				h.routeRingGroup(r.Context(), w, info, e164.DialDigits(normalized), "public")
				return
			}
			// Phase 3 Wave 2: voicemail destination.
			if vm, vmErr := h.store.LookupDIDVoicemailTarget(r.Context(), normalized); vmErr == nil {
				h.routeDIDVoicemail(w, vm, e164.DialDigits(normalized))
				return
			}
			// Phase 3 Wave 3: IVR destination.
			if ivr, ivrErr := h.store.LookupDIDIVRTarget(r.Context(), normalized); ivrErr == nil {
				h.routeIVR(w, ivr, e164.DialDigits(normalized), "public")
				return
			}
			// Phase 3 Wave 4: queue destination.
			if q, qErr := h.store.LookupDIDQueueTarget(r.Context(), normalized); qErr == nil {
				h.routeQueue(w, q, e164.DialDigits(normalized), "public")
				return
			}
			slog.Info("inbound DID not provisioned", "did", normalized)
			writeHangup(w, "public", "UNALLOCATED_NUMBER")
			return
		}
		slog.Error("DID lookup failed", "did", normalized, "err", err)
		writeHangup(w, "public", "TEMPORARY_FAILURE")
		return
	}

	bridgeURI := fmt.Sprintf("sofia/internal/sip:%s@%s;fs_path=sip:%s;lr",
		target.SIPUsername, target.SIPDomain, h.sipTarget)

	writeDialplan(w, dialplanData{
		Context: "public",
		Name:    "inbound-" + e164.DialDigits(normalized),
		Actions: []dialplanAction{
			{App: "set", Data: "hangup_after_bridge=true"},
			{App: "set", Data: "x_call_direction=inbound"},
			{App: "set", Data: "x_tenant_id=" + target.TenantID.String()},
			{App: "set", Data: "x_did=" + normalized},
			{App: "set", Data: "x_did_id=" + target.DIDID.String()},
			{App: "bridge", Data: bridgeURI},
		},
	})
}

// handleVoicemailCheck serves *97 — the caller checks their own voicemail.
// The caller's extension number comes from Caller-Caller-ID-Number; FS
// then prompts for the PIN (looked up via the directory handler).
func (h *Handler) handleVoicemailCheck(w http.ResponseWriter, r *http.Request, tenantDomain string) {
	caller := r.FormValue("Caller-Caller-ID-Number")
	if tenantDomain == "" || caller == "" {
		writeHangup(w, "default", "USER_NOT_REGISTERED")
		return
	}
	writeDialplan(w, dialplanData{
		Context: "default",
		Name:    "voicemail-check",
		Actions: []dialplanAction{
			{App: "answer"},
			{App: "sleep", Data: "500"},
			{App: "voicemail", Data: fmt.Sprintf("check default %s %s", tenantDomain, caller)},
		},
	})
}

// routeDIDVoicemail sends an inbound PSTN call straight to a voicemail box
// (DID destination_kind = 'voicemail').
func (h *Handler) routeDIDVoicemail(w http.ResponseWriter, t *store.DIDVoicemailTarget, displayDest string) {
	writeDialplan(w, dialplanData{
		Context: "public",
		Name:    "inbound-vm-" + displayDest,
		Actions: []dialplanAction{
			{App: "set", Data: "hangup_after_bridge=true"},
			{App: "set", Data: "x_call_direction=inbound"},
			{App: "set", Data: "x_tenant_id=" + t.TenantID.String()},
			{App: "set", Data: "x_voicemail_box_id=" + t.BoxID.String()},
			{App: "answer"},
			{App: "sleep", Data: "500"},
			{App: "voicemail", Data: fmt.Sprintf("default %s %s", t.SIPDomain, t.SIPUsername)},
		},
	})
}

// buildCFFallbackActions returns the post-bridge fallback chain for an
// extension based on its CF + voicemail settings. Caller composes this onto
// the action list after the bridge action.
//
// Pure function — no http.ResponseWriter, no I/O — so it's easy to unit-test.
func buildCFFallbackActions(ext *store.Extension, domain string) []dialplanAction {
	hasBusy := ext.CFBusy != ""
	hasNoAns := ext.CFNoAnswer != ""

	switch {
	case hasBusy && hasNoAns:
		// FS ${cond(...)} evaluates at runtime; originate_disposition is set
		// by the bridge app on completion. USER_BUSY → busy target, else
		// → no-answer target.
		return []dialplanAction{
			{App: "set", Data: fmt.Sprintf(
				"_cf_target=${cond(${originate_disposition} == USER_BUSY ? %s : %s)}",
				ext.CFBusy, ext.CFNoAnswer,
			)},
			{App: "transfer", Data: "${_cf_target} XML default"},
		}
	case hasNoAns:
		return []dialplanAction{
			{App: "transfer", Data: ext.CFNoAnswer + " XML default"},
		}
	case hasBusy:
		// Branch: busy → CF target; anything else → voicemail (if enabled) or hangup.
		out := []dialplanAction{
			{App: "set", Data: fmt.Sprintf(
				"_cf_target=${cond(${originate_disposition} == USER_BUSY ? %s : )}",
				ext.CFBusy,
			)},
			// transfer with empty data is a no-op; FS skips and continues.
			{App: "transfer", Data: "${_cf_target} XML default"},
		}
		if ext.VoicemailEnabled {
			out = append(out,
				dialplanAction{App: "answer"},
				dialplanAction{App: "sleep", Data: "500"},
				dialplanAction{App: "voicemail", Data: fmt.Sprintf("default %s %s", domain, ext.SIPUsername)},
			)
		}
		return out
	case ext.VoicemailEnabled:
		return []dialplanAction{
			{App: "answer"},
			{App: "sleep", Data: "500"},
			{App: "voicemail", Data: fmt.Sprintf("default %s %s", domain, ext.SIPUsername)},
		}
	}
	return nil
}

// routeQueue dispatches a call into a mod_callcenter queue. The queue itself
// (and its agents + tiers) is provisioned via the configuration handler
// (callcenter.conf).
func (h *Handler) routeQueue(w http.ResponseWriter, q *store.Queue, displayDest, context string) {
	direction := "internal"
	if context == "public" {
		direction = "inbound"
	}
	writeDialplan(w, dialplanData{
		Context: context,
		Name:    "queue-" + displayDest,
		Actions: []dialplanAction{
			{App: "set", Data: "x_call_direction=" + direction},
			{App: "set", Data: "x_tenant_id=" + q.TenantID.String()},
			{App: "set", Data: "x_queue_id=" + q.ID.String()},
			{App: "answer"},
			{App: "callcenter", Data: q.ID.String()},
		},
	})
}

// routeIVR returns dialplan XML that answers the call and runs the IVR
// menu identified by ivr.id. mod_ivr will fetch the menu definition via
// the configuration handler (ivr.conf).
func (h *Handler) routeIVR(w http.ResponseWriter, v *store.IVR, displayDest, context string) {
	direction := "internal"
	if context == "public" {
		direction = "inbound"
	}
	writeDialplan(w, dialplanData{
		Context: context,
		Name:    "ivr-" + displayDest,
		Actions: []dialplanAction{
			{App: "set", Data: "x_call_direction=" + direction},
			{App: "set", Data: "x_tenant_id=" + v.TenantID.String()},
			{App: "set", Data: "x_ivr_id=" + v.ID.String()},
			{App: "answer"},
			{App: "sleep", Data: "500"},
			{App: "ivr", Data: v.ID.String()},
		},
	})
}

// routeRingGroup renders the dialplan that bridges a call to all enabled
// members of a ring group. Called from both default (internal dial) and
// public (inbound DID with destination_kind=ring_group) contexts.
func (h *Handler) routeRingGroup(ctx context.Context, w http.ResponseWriter, info *store.RingGroupRoutingInfo, displayDest, context string) {
	if !supportedRingGroupStrategy(info.Group.Strategy) {
		slog.Warn("ring group strategy not yet wired", "strategy", info.Group.Strategy, "group", info.Group.ID)
		writeHangup(w, context, "SERVICE_UNAVAILABLE")
		return
	}

	// Wave 1.5: round-robin and random reorder members, then render as
	// sequential. simultaneous/sequential keep the DB ordering.
	members := info.Members
	renderStrategy := info.Group.Strategy
	switch info.Group.Strategy {
	case "round_robin":
		idx, err := h.store.NextRingGroupRRIndex(ctx, info.Group.ID, len(members))
		if err != nil {
			slog.Warn("rr index lookup failed; falling back to no rotation",
				"group", info.Group.ID, "err", err)
		} else {
			members = rotateMembers(members, idx)
		}
		renderStrategy = "sequential"
	case "random":
		members = shuffleMembers(members)
		renderStrategy = "sequential"
	}

	dialstring := buildRingGroupDialString(members, renderStrategy, info.Group.RingTimeoutSec, h.sipTarget)
	if dialstring == "" {
		slog.Info("ring group has no enabled members", "group", info.Group.ID)
		writeHangup(w, context, "NO_USER_RESPONSE")
		return
	}

	direction := "internal"
	if context == "public" {
		direction = "inbound"
	}

	actions := []dialplanAction{
		{App: "set", Data: "hangup_after_bridge=true"},
		{App: "set", Data: "continue_on_fail=true"},
		{App: "set", Data: fmt.Sprintf("call_timeout=%d", info.Group.RingTimeoutSec)},
		{App: "set", Data: "x_call_direction=" + direction},
		{App: "set", Data: "x_tenant_id=" + info.Group.TenantID.String()},
		{App: "set", Data: "x_ring_group_id=" + info.Group.ID.String()},
	}
	if info.Group.CallerIDPrefix != "" {
		// FS evaluates ${caller_id_name} at bridge time.
		actions = append(actions, dialplanAction{
			App:  "set",
			Data: fmt.Sprintf("effective_caller_id_name=%s${caller_id_name}", info.Group.CallerIDPrefix),
		})
	}
	actions = append(actions, dialplanAction{App: "bridge", Data: dialstring})

	writeDialplan(w, dialplanData{
		Context: context,
		Name:    "ringgroup-" + displayDest,
		Actions: actions,
	})
}

// ---------------------------------------------------------------------------
// XML rendering
// ---------------------------------------------------------------------------

type dialplanData struct {
	Context string
	Name    string
	Actions []dialplanAction
}

type dialplanAction struct {
	App  string
	Data string
}

var dialplanTmpl = template.Must(template.New("dialplan").Parse(`<?xml version="1.0"?>
<document type="freeswitch/xml">
  <section name="dialplan">
    <context name="{{.Context}}">
      <extension name="{{.Name}}">
        <condition>
{{range .Actions}}          <action application="{{.App}}" data="{{.Data}}"/>
{{end}}        </condition>
      </extension>
    </context>
  </section>
</document>
`))

var hangupTmpl = template.Must(template.New("hangup").Parse(`<?xml version="1.0"?>
<document type="freeswitch/xml">
  <section name="dialplan">
    <context name="{{.Context}}">
      <extension name="hangup">
        <condition>
          <action application="hangup" data="{{.Cause}}"/>
        </condition>
      </extension>
    </context>
  </section>
</document>
`))

func writeDialplan(w http.ResponseWriter, d dialplanData) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	_ = dialplanTmpl.Execute(w, d)
}

func writeHangup(w http.ResponseWriter, context, cause string) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	_ = hangupTmpl.Execute(w, struct {
		Context, Cause string
	}{context, cause})
}

func writeNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	_, _ = w.Write([]byte(`<?xml version="1.0"?>
<document type="freeswitch/xml">
  <section name="result"><result status="not found"/></section>
</document>`))
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
