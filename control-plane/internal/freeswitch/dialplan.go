package freeswitch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tendpos/sip-platform/control-plane/internal/e164"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
	"github.com/tendpos/sip-platform/control-plane/internal/webhook"
)

// Handler responds to FreeSWITCH mod_xml_curl dialplan lookups.
//
// Phase 2 routing matrix:
//
//	context=default + destination is internal extension → bridge via Kamailio
//	context=default + destination looks like E.164      → outbound PSTN via gateway
//	context=public  (carrier gateways' inbound context) → DID lookup → inbound to extension
type Handler struct {
	store     *store.Store
	sipTarget string              // host:port of Kamailio
	webhooks  *webhook.Dispatcher // Kari's Law emergency.dialed notifications (nil-safe)
}

func NewHandler(s *store.Store, kamailioSIPTarget string, webhooks *webhook.Dispatcher) *Handler {
	return &Handler{store: s, sipTarget: kamailioSIPTarget, webhooks: webhooks}
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
	// Kari's Law: 911 must dial directly (no prefix) and must NEVER be shadowed
	// by extension/external routing, so emergency detection comes first.
	if isEmergencyNumber(destNum) {
		h.routeEmergency(w, r, destNum, tenantDomain)
		return
	}

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
				if pg, pgErr := h.store.LookupPagingGroupByExtension(ctx, tenantDomain, destNum); pgErr == nil {
					h.routePaging(w, pg, destNum, "default")
					return
				}
				if cr, crErr := h.store.LookupConferenceRoomByExtension(ctx, tenantDomain, destNum); crErr == nil {
					h.routeConference(w, cr, destNum, "default")
					return
				}
				// Call park: a feature code (e.g. *68) parks the call into an
				// auto-assigned orbit slot; a bare slot number retrieves it.
				if lot, plErr := h.store.LookupParkLotByFeatureCode(ctx, tenantDomain, destNum); plErr == nil {
					h.routePark(w, lot, destNum, "default")
					return
				}
				if slot, ok := parseSlot(destNum); ok {
					if lot, plErr := h.store.LookupParkLotBySlot(ctx, tenantDomain, slot); plErr == nil {
						h.routeParkRetrieve(w, lot, slot, destNum, "default")
						return
					}
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

	// Per-tenant Music on Hold: override the default hold_music when the tenant
	// has a custom source. Best-effort, cosmetic — never affects routing.
	if moh := h.store.TenantMoHByDomain(ctx, domain); moh != "" {
		actions = append(actions, dialplanAction{App: "set", Data: "hold_music=" + moh})
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

// emergencyNumbers is the fixed set of digit strings that must route as an
// emergency call. 933 is the FCC address-verification test line (it reads back
// the dispatchable location the carrier has on file). New entries here are the
// only place emergency dialing is defined.
var emergencyNumbers = map[string]bool{
	"911": true,
	"933": true,
}

// isEmergencyNumber reports whether the dialed digits are an emergency number.
// Pure helper — no I/O — so it's trivially unit-testable.
func isEmergencyNumber(s string) bool {
	return emergencyNumbers[s]
}

// routeEmergency handles a 911 (or 933 test) call. It resolves the caller's
// dispatchable location, bridges the call OUT to the tenant's carrier using the
// same gateway-selection logic as outbound PSTN (but dialing the literal "911"
// — NOT an E.164 number), stamps the dispatchable address onto the channel for
// RAY BAUM's Act, and fires the Kari's Law emergency.dialed notification.
func (h *Handler) routeEmergency(w http.ResponseWriter, r *http.Request, destNum, tenantDomain string) {
	ctx := r.Context()
	caller := r.FormValue("Caller-Caller-ID-Number")

	// Resolve the calling extension's tenant + dispatchable address. This uses a
	// dedicated query (NOT the main Extension scan) so reading the e911 column
	// never ripples into the rest of the routing code.
	var (
		tenantID  uuid.UUID
		ext       = caller
		addrLine  string
		resolveOK bool
	)
	if tenantDomain != "" && caller != "" {
		if r, err := h.store.ResolveE911ForExtensionNumber(ctx, tenantDomain, caller); err == nil {
			resolveOK = true
			tenantID = r.TenantID
			ext = r.Extension
			if r.Address != nil {
				addrLine = r.Address.SingleLine()
			}
		} else {
			slog.Warn("e911: could not resolve calling extension; routing without dispatchable address",
				"caller", caller, "tenant_domain", tenantDomain, "err", err)
		}
	}
	if addrLine == "" {
		slog.Warn("e911: emergency call has no dispatchable location assigned",
			"caller", caller, "tenant_domain", tenantDomain, "dialed", destNum)
	}

	// Pick the tenant's carrier the same way outbound PSTN does. If we couldn't
	// resolve the extension's tenant, fall back to the platform-wide primary.
	var acct *store.CarrierAccount
	if resolveOK && tenantID != uuid.Nil {
		if a, perr := h.store.PickPrimaryCarrierAccountForTenant(ctx, tenantID); perr == nil {
			acct = a
		} else if !errors.Is(perr, pgx.ErrNoRows) {
			slog.Warn("e911: tenant carrier lookup failed; will fall back", "tenant", tenantID, "err", perr)
		}
	} else if tenantDomain != "" {
		if tenant, terr := h.store.GetTenantBySIPDomain(ctx, tenantDomain); terr == nil && tenant != nil {
			if a, perr := h.store.PickPrimaryCarrierAccountForTenant(ctx, tenant.ID); perr == nil {
				acct = a
				if tenantID == uuid.Nil {
					tenantID = tenant.ID
				}
			}
		}
	}
	if acct == nil {
		if a, perr := h.store.PickPrimaryCarrierAccount(ctx); perr == nil {
			acct = a
		}
	}

	// Fire the Kari's Law notification regardless of whether a carrier is
	// configured — a central point must learn that 911 was dialed.
	if h.webhooks != nil && tenantID != uuid.Nil {
		h.webhooks.Fire(tenantID, "emergency.dialed", map[string]any{
			"dialed":        destNum,
			"extension":     ext,
			"caller_id_num": caller,
			"address":       addrLine,
		})
	}

	if acct == nil {
		// No carrier to route to. Don't silently drop: answer, play a notice, and
		// hang up so the caller hears something. Best-effort (a logged error +
		// hangup is acceptable for this edge per spec).
		slog.Error("e911: no carrier configured for emergency call — cannot reach PSAP",
			"caller", caller, "tenant_domain", tenantDomain, "dialed", destNum)
		writeDialplan(w, dialplanData{
			Context: "default",
			Name:    "emergency-no-carrier-" + destNum,
			Actions: []dialplanAction{
				{App: "set", Data: "x_emergency=true"},
				{App: "answer"},
				{App: "sleep", Data: "500"},
				{App: "playback", Data: "ivr/ivr-call_cannot_be_completed_as_dialed.wav"},
				{App: "hangup", Data: "NO_ROUTE_DESTINATION"},
			},
		})
		return
	}

	writeDialplan(w, dialplanData{
		Context: "default",
		Name:    "emergency-" + destNum,
		Actions: buildEmergencyActions(acct.FSGatewayName, ext, addrLine),
	})
}

// buildEmergencyActions is the pure builder for the 911 dialplan. It stamps the
// emergency markers + dispatchable address onto the channel and bridges the
// literal "911" out through the carrier gateway (911 is NOT E.164, so it is
// dialed verbatim — never normalized). Pure function — no I/O — for unit tests.
func buildEmergencyActions(gatewayName, ext, address string) []dialplanAction {
	return []dialplanAction{
		{App: "set", Data: "hangup_after_bridge=true"},
		{App: "set", Data: "x_call_direction=emergency"},
		{App: "set", Data: "x_emergency=true"},
		{App: "set", Data: "x_e911_extension=" + ext},
		{App: "set", Data: "x_e911_address=" + address},
		{App: "bridge", Data: "sofia/gateway/" + gatewayName + "/911"},
	}
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

	// Business hours: when the DID has a schedule that is CLOSED right now,
	// route to its closed destination instead of the normal one. DIDs with no
	// schedule (the common case) are unaffected — ResolveScheduledClosedDestination
	// returns pgx.ErrNoRows / nil and we fall straight through.
	if cd, cerr := h.store.ResolveScheduledClosedDestination(r.Context(), normalized, time.Now()); cerr == nil && cd != nil {
		if h.routeClosedDestination(r.Context(), w, cd, normalized) {
			return
		}
		slog.Warn("closed-destination routing failed; using normal routing", "did", normalized, "kind", cd.Kind)
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

	inboundActions := []dialplanAction{
		{App: "set", Data: "hangup_after_bridge=true"},
		{App: "set", Data: "x_call_direction=inbound"},
		{App: "set", Data: "x_tenant_id=" + target.TenantID.String()},
		{App: "set", Data: "x_did=" + normalized},
		{App: "set", Data: "x_did_id=" + target.DIDID.String()},
	}
	if moh := h.store.TenantMoHByDomain(r.Context(), target.SIPDomain); moh != "" {
		inboundActions = append(inboundActions, dialplanAction{App: "set", Data: "hold_music=" + moh})
	}
	inboundActions = append(inboundActions, dialplanAction{App: "bridge", Data: bridgeURI})

	writeDialplan(w, dialplanData{
		Context: "public",
		Name:    "inbound-" + e164.DialDigits(normalized),
		Actions: inboundActions,
	})
}

// routeClosedDestination routes an inbound call to a DID's after-hours
// destination (business-hours feature). Returns false if the configured
// destination can't be resolved, so the caller falls back to normal routing.
func (h *Handler) routeClosedDestination(ctx context.Context, w http.ResponseWriter, cd *store.ScheduledClosedDestination, normalized string) bool {
	dest := e164.DialDigits(normalized)
	switch cd.Kind {
	case "extension":
		t, err := h.store.ExtensionRouteByID(ctx, cd.ID)
		if err != nil {
			return false
		}
		bridgeURI := fmt.Sprintf("sofia/internal/sip:%s@%s;fs_path=sip:%s;lr",
			t.SIPUsername, t.SIPDomain, h.sipTarget)
		actions := []dialplanAction{
			{App: "set", Data: "hangup_after_bridge=true"},
			{App: "set", Data: "x_call_direction=inbound"},
			{App: "set", Data: "x_tenant_id=" + t.TenantID.String()},
			{App: "set", Data: "x_did=" + normalized},
			{App: "set", Data: "x_closed_route=1"},
		}
		if moh := h.store.TenantMoHByDomain(ctx, t.SIPDomain); moh != "" {
			actions = append(actions, dialplanAction{App: "set", Data: "hold_music=" + moh})
		}
		actions = append(actions, dialplanAction{App: "bridge", Data: bridgeURI})
		writeDialplan(w, dialplanData{Context: "public", Name: "inbound-closed-" + dest, Actions: actions})
		return true
	case "ring_group":
		info, err := h.store.RingGroupRouteByID(ctx, cd.ID)
		if err != nil {
			return false
		}
		h.routeRingGroup(ctx, w, info, dest, "public")
		return true
	case "voicemail":
		vm, err := h.store.VoicemailRouteByID(ctx, cd.ID)
		if err != nil {
			return false
		}
		h.routeDIDVoicemail(w, vm, dest)
		return true
	case "ivr":
		ivr, err := h.store.IVRByID(ctx, cd.ID)
		if err != nil {
			return false
		}
		h.routeIVR(w, ivr, dest, "public")
		return true
	case "queue":
		q, err := h.store.QueueByID(ctx, cd.ID)
		if err != nil {
			return false
		}
		h.routeQueue(w, q, dest, "public")
		return true
	}
	return false
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

// routePaging renders a one-way intercom page: the caller is dropped into a
// `…@paging` conference and every group member is auto-outcalled muted with
// hands-free auto-answer, so the caller talks and members only listen. Works
// on any registered phone/softphone regardless of the group's `mode` (multicast
// and native PTT layer additional out-of-band delivery on top of this; see
// increments 3/4). The conference ends when the pager hangs up.
func (h *Handler) routePaging(w http.ResponseWriter, info *store.PagingRoutingInfo, displayDest, context string) {
	actions := buildPagingActions(info, h.sipTarget)
	if actions == nil {
		slog.Info("paging group has no active members", "group", info.Group.ID)
		writeHangup(w, context, "NO_USER_RESPONSE")
		return
	}
	writeDialplan(w, dialplanData{
		Context: context,
		Name:    "paging-" + displayDest,
		Actions: actions,
	})
}

// buildPagingActions is the pure builder for the conference-page dialplan.
// Returns nil if the group has no members to page.
func buildPagingActions(info *store.PagingRoutingInfo, sipTarget string) []dialplanAction {
	if len(info.Members) == 0 {
		return nil
	}
	// One muted, auto-answering outcall per member. mod_conference splits the
	// auto-outcall list on spaces and dials each leg independently; the prefix
	// var prepends sip_auto_answer so phones pick up hands-free.
	dials := make([]string, 0, len(info.Members))
	for _, m := range info.Members {
		dials = append(dials, fmt.Sprintf("sofia/internal/sip:%s@%s;fs_path=sip:%s;lr",
			m.SIPUsername, m.SIPDomain, sipTarget))
	}
	confName := "paging_" + info.Group.ID.String()
	return []dialplanAction{
		{App: "set", Data: "x_call_direction=internal"},
		{App: "set", Data: "x_tenant_id=" + info.Group.TenantID.String()},
		{App: "set", Data: "x_paging_group_id=" + info.Group.ID.String()},
		{App: "answer"},
		// Short "go ahead" tone so the pager knows when to start talking.
		// Played to the caller only, before they join the conference.
		{App: "playback", Data: "tone_stream://%(200,0,800)"},
		{App: "set", Data: "conference_auto_outcall_timeout=30"},
		// Called members are muted → one-way page (caller talks, members listen).
		{App: "set", Data: "conference_auto_outcall_flags=mute"},
		{App: "set", Data: "conference_auto_outcall_caller_id_name=Paging"},
		{App: "set", Data: "conference_auto_outcall_prefix={sip_auto_answer=true,ignore_early_media=true}"},
		{App: "conference_set_auto_outcall", Data: strings.Join(dials, " ")},
		// Pager joins as moderator; endconf tears the page down when they leave.
		{App: "conference", Data: confName + "@paging+flags{endconf|moderator}"},
	}
}

// routeConference renders the dialplan for a meet-me conference bridge: answer,
// optionally collect + verify a PIN, then join the FreeSWITCH `default`
// conference profile. The conference name is namespaced per tenant + room
// extension so two tenants reusing the same room number never collide.
func (h *Handler) routeConference(w http.ResponseWriter, room *store.ConferenceRoom, displayDest, context string) {
	direction := "internal"
	if context == "public" {
		direction = "inbound"
	}
	writeDialplan(w, dialplanData{
		Context: context,
		Name:    "conference-" + displayDest,
		Actions: buildConferenceActions(room, direction),
	})
}

// buildConferenceActions is the pure builder for the meet-me conference
// dialplan. It answers the call, optionally prompts for a PIN, and joins the
// `default` conference profile under a per-tenant/room name.
//
//   - No PIN configured            → join directly (member).
//   - Member PIN only              → prompt; on match join as member.
//   - Member + moderator PIN        → prompt; moderator PIN joins with
//     +flags{moderator|endconf}, member PIN joins as a plain member. The
//     play_and_get_digits regex matches EITHER pin, and a runtime ${cond}
//     selects the flags based on which one was entered.
//   - Moderator PIN only            → prompt; on match join as moderator.
//
// record=true adds record_session; max_members>0 caps the room; announce_count
// toggles the enter/exit member-count announcement (the default profile
// announces by default, so we only need to suppress it when false).
func buildConferenceActions(room *store.ConferenceRoom, direction string) []dialplanAction {
	confName := room.TenantID.String() + "_" + room.Extension
	memberFlags := ""
	moderatorFlags := "moderator|endconf"

	actions := []dialplanAction{
		{App: "set", Data: "x_call_direction=" + direction},
		{App: "set", Data: "x_tenant_id=" + room.TenantID.String()},
		{App: "set", Data: "x_conference_room_id=" + room.ID.String()},
		{App: "answer"},
		{App: "sleep", Data: "500"},
	}

	if room.MaxMembers > 0 {
		actions = append(actions, dialplanAction{
			App:  "set",
			Data: fmt.Sprintf("conference_max_members=%d", room.MaxMembers),
		})
	}
	if !room.AnnounceCount {
		// Suppress the default profile's enter/exit count announcement.
		actions = append(actions, dialplanAction{App: "set", Data: "conference_utils_auto_no_video=true"})
	}

	// Channel var holding the flags we'll join the conference with.
	flags := memberFlags

	switch {
	case room.PIN == "" && room.ModeratorPIN == "":
		// No PIN — join straight in.
	case room.PIN != "" && room.ModeratorPIN != "":
		// Accept either PIN; pick flags by which one was entered.
		regex := room.PIN + "|" + room.ModeratorPIN
		actions = append(actions, dialplanAction{
			App:  "play_and_get_digits",
			Data: fmt.Sprintf("1 12 3 5000 # silence_stream://250 silence_stream://250 conf_pin %s", regex),
		})
		actions = append(actions, dialplanAction{
			App: "set",
			Data: fmt.Sprintf("conf_flags=${cond(${conf_pin} == %s ? %s : %s)}",
				room.ModeratorPIN, moderatorFlags, memberFlags),
		})
		flags = "${conf_flags}"
	case room.ModeratorPIN != "":
		// Moderator-only PIN: prompt, and everyone who knows it is a moderator.
		actions = append(actions, dialplanAction{
			App:  "play_and_get_digits",
			Data: fmt.Sprintf("1 12 3 5000 # silence_stream://250 silence_stream://250 conf_pin %s", room.ModeratorPIN),
		})
		flags = moderatorFlags
	default:
		// Member-only PIN.
		actions = append(actions, dialplanAction{
			App:  "play_and_get_digits",
			Data: fmt.Sprintf("1 12 3 5000 # silence_stream://250 silence_stream://250 conf_pin %s", room.PIN),
		})
	}

	if room.Record {
		recPath := fmt.Sprintf("$${recordings_dir}/%s/${strftime(%%Y-%%m-%%d)}/conf-%s-${uuid}.wav",
			room.TenantID.String(), room.Extension)
		actions = append(actions,
			dialplanAction{App: "set", Data: "recording_path=" + recPath},
			dialplanAction{App: "record_session", Data: "${recording_path}"},
		)
	}

	confData := confName + "@default"
	if flags != "" {
		confData += "+flags{" + flags + "}"
	}
	actions = append(actions, dialplanAction{App: "conference", Data: confData})
	return actions
}

// parseSlot returns the integer value of an all-digit destination number (a
// park-retrieve slot). Anything containing a non-digit (feature codes like *68,
// extensions, E.164) returns ok=false so it never gets treated as a slot.
func parseSlot(dest string) (int, bool) {
	if dest == "" {
		return 0, false
	}
	for _, r := range dest {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(dest)
	if err != nil {
		return 0, false
	}
	return n, true
}

// valetLotID namespaces a park lot's valet-parking lot id per tenant so two
// tenants reusing the same lot name never share orbit slots. The name is
// scrubbed to ascii-alnum/underscore (valet lot ids are referenced verbatim in
// the dialplan, so we keep them simple and collision-resistant).
func valetLotID(tenantID uuid.UUID, name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_':
			b.WriteRune(r)
		}
	}
	return strings.ReplaceAll(tenantID.String(), "-", "") + "_" + b.String()
}

// routePark renders the dialplan for parking an in-progress call: answer, then
// hand the call to mod_valet_parking in auto-assign mode, which picks the next
// free slot in [slot_start, slot_end] and announces it to the parking party.
//
// NOTE: requires mod_valet_parking loaded on the FreeSWITCH box (see
// freeswitch/conf/autoload_configs/modules.conf.xml; deploying that to the box
// is an ops concern handled separately).
func (h *Handler) routePark(w http.ResponseWriter, lot *store.ParkLot, displayDest, context string) {
	writeDialplan(w, dialplanData{
		Context: context,
		Name:    "park-" + displayDest,
		Actions: buildParkActions(lot),
	})
}

// routeParkRetrieve renders the dialplan for retrieving a call parked in a
// specific orbit slot: valet_park with an explicit slot bridges the caller to
// whoever is parked there.
func (h *Handler) routeParkRetrieve(w http.ResponseWriter, lot *store.ParkLot, slot int, displayDest, context string) {
	writeDialplan(w, dialplanData{
		Context: context,
		Name:    "park-retrieve-" + displayDest,
		Actions: buildRetrieveActions(lot, slot),
	})
}

// buildParkActions is the pure builder for parking a call. It answers and then
// calls valet_park in "auto in" mode, letting mod_valet_parking auto-assign the
// next free slot in the lot's range and announce it.
func buildParkActions(lot *store.ParkLot) []dialplanAction {
	lotID := valetLotID(lot.TenantID, lot.Name)
	return []dialplanAction{
		{App: "set", Data: "x_call_direction=internal"},
		{App: "set", Data: "x_tenant_id=" + lot.TenantID.String()},
		{App: "set", Data: "x_park_lot_id=" + lot.ID.String()},
		{App: "answer"},
		{App: "valet_park", Data: fmt.Sprintf("%s auto in %d %d", lotID, lot.SlotStart, lot.SlotEnd)},
	}
}

// buildRetrieveActions is the pure builder for retrieving a parked call from an
// explicit orbit slot.
func buildRetrieveActions(lot *store.ParkLot, slot int) []dialplanAction {
	lotID := valetLotID(lot.TenantID, lot.Name)
	return []dialplanAction{
		{App: "set", Data: "x_call_direction=internal"},
		{App: "set", Data: "x_tenant_id=" + lot.TenantID.String()},
		{App: "set", Data: "x_park_lot_id=" + lot.ID.String()},
		{App: "valet_park", Data: fmt.Sprintf("%s %d", lotID, slot)},
	}
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
