package freeswitch

import (
	"log/slog"
	"net/http"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// routeQueueCallback renders the callback-request offer for a queue. A caller
// reaches this via *8<queue_extension> (see handleDefault). Admins point a
// queue's overflow / timeout / an IVR option at *8<ext> (e.g. *8700 for queue
// 700). The dialplan answers, offers "press 1 for a callback", and:
//
//   - on opt-in (digit 1): a pending callback is recorded for this caller +
//     queue, a confirmation plays, and the call hangs up;
//   - otherwise: the caller is transferred into the queue's own extension so
//     they hold normally.
//
// Trigger design: a pure dialed-destination form (*8<ext>) so it composes with
// existing IVR/overflow routing and needs no new channel-var plumbing.
//
// Phase-A pragmatic choice: because the xml_curl handler is stateless (FS
// re-requests per action block and we can't branch on a captured digit inside a
// single XML document), we persist the callback up-front here — the caller
// signalled intent by dialing *8<ext>. The dialplan then plays a confirmation
// and lets a non-1 keypress fall through to the queue. The deeper "press 1
// mid-queue capture inside mod_callcenter" remains the flagged follow-up.
func (h *Handler) routeQueueCallback(w http.ResponseWriter, r *http.Request, q *store.Queue, displayDest string) {
	caller := firstNonEmpty(
		r.FormValue("Caller-Caller-ID-Number"),
		r.FormValue("variable_sip_from_user"),
	)
	callerName := r.FormValue("Caller-Caller-ID-Name")

	if caller == "" {
		// No usable number to call back — fall back to joining the queue normally.
		slog.Info("queue callback requested without caller id; routing to queue",
			"queue", q.ID, "dest", displayDest)
		h.routeQueue(w, q, q.Extension, "default")
		return
	}

	// Record the pending callback. Best-effort: if the store call fails we still
	// answer + confirm rather than dropping the caller (they dialed in good faith).
	if _, err := h.store.RequestQueueCallback(r.Context(), q.TenantID, q.ID, caller, callerName); err != nil {
		slog.Warn("queue callback persist failed; offering anyway",
			"queue", q.ID, "caller", caller, "err", err)
	} else {
		slog.Info("queue callback recorded", "queue", q.ID, "caller", caller)
	}

	writeDialplan(w, dialplanData{
		Context: "default",
		Name:    "queue-callback-" + displayDest,
		Actions: buildCallbackOfferActions(q),
	})
}

// buildCallbackOfferActions is the pure builder for the queue-callback offer
// dialplan. It answers the call, offers the callback ("press 1"), then:
//
//   - captures one digit via play_and_get_digits into ${pagd_input};
//   - if the caller pressed 1 → plays a "we'll call you back" confirmation and
//     hangs up;
//   - otherwise (or if the queue has a dialable extension) → transfers the
//     caller into the queue extension so they hold normally.
//
// The branch is expressed with FreeSWITCH's inline ${cond()} on ${pagd_input}
// so the whole flow fits one stateless XML document.
//
// NOTE: the prompt sound files (ivr/ivr-callback_*.wav) are PLACEHOLDERS — wire
// real prompts before GA.
func buildCallbackOfferActions(q *store.Queue) []dialplanAction {
	queueExt := q.Extension

	actions := []dialplanAction{
		{App: "set", Data: "x_call_direction=internal"},
		{App: "set", Data: "x_tenant_id=" + q.TenantID.String()},
		{App: "set", Data: "x_queue_id=" + q.ID.String()},
		{App: "set", Data: "x_queue_callback_offer=true"},
		{App: "answer"},
		{App: "sleep", Data: "500"},
		// Offer: capture exactly one digit (min 1, max 1, 1 try, 5s timeout).
		// ${pagd_input} holds the captured digit ("1" on opt-in). PLACEHOLDER
		// prompt files for the offer + the invalid re-prompt.
		{App: "play_and_get_digits", Data: "1 1 1 5000 # " +
			"ivr/ivr-callback_offer.wav ivr/ivr-callback_invalid.wav pagd_input \\d+"},
		// Opt-in confirmation: only plays when the caller pressed 1. ${cond()}
		// keeps the whole branch in one stateless document. PLACEHOLDER prompt.
		{App: "playback", Data: "${cond(${pagd_input} == 1 ? ivr/ivr-callback_confirm.wav : silence_stream://10)}"},
	}

	if queueExt != "" {
		// Non-opt-in (didn't press 1) → transfer into the queue extension so they
		// hold normally. On opt-in the transfer target is empty (a no-op) and the
		// call hangs up after the confirmation.
		actions = append(actions, dialplanAction{
			App:  "transfer",
			Data: "${cond(${pagd_input} == 1 ? : " + queueExt + " XML default)}",
		})
	}
	return actions
}
