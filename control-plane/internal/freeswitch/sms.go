package freeswitch

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/tendpos/sip-platform/control-plane/internal/e164"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// SMSInboundHandler receives inbound text messages from FreeSWITCH (a chatplan
// posts here when a SIP MESSAGE arrives on a carrier trunk). It resolves the
// tenant from the destination DID and stores the message.
//
// Contract (form or query): from=<peer E.164>, to=<our DID E.164>, body=<text>.
// Lives on the internal admin listener alongside the other FS callbacks.
type SMSInboundHandler struct {
	store *store.Store
}

func NewSMSInboundHandler(st *store.Store) *SMSInboundHandler {
	return &SMSInboundHandler{store: st}
}

func (h *SMSInboundHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	from := normalizeSMSNumber(r.FormValue("from"))
	to := normalizeSMSNumber(r.FormValue("to"))
	body := r.FormValue("body")
	if from == "" || to == "" || body == "" {
		http.Error(w, "from, to, body required", http.StatusBadRequest)
		return
	}
	tid, err := h.store.TenantIDByDID(r.Context(), to)
	if err != nil {
		slog.Info("inbound SMS for unknown DID", "to", to, "from", from)
		http.Error(w, "unknown destination", http.StatusNotFound)
		return
	}
	if _, err := h.store.CreateSMS(r.Context(), store.CreateSMSInput{
		TenantID: tid, OurE164: to, PeerE164: from,
		Direction: "inbound", Body: body, Status: "received",
	}); err != nil {
		slog.Error("store inbound SMS", "err", err)
		http.Error(w, "store failed", http.StatusInternalServerError)
		return
	}
	slog.Info("inbound SMS stored", "tenant", tid, "to", to, "from", from, "len", len(body))
	w.WriteHeader(http.StatusNoContent)
}

func normalizeSMSNumber(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if n, err := e164.Normalize(s, "US"); err == nil {
		if !strings.HasPrefix(n, "+") {
			n = "+" + n
		}
		return n
	}
	return s
}
