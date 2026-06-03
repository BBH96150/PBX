package portal

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/audit"
	"github.com/tendpos/sip-platform/control-plane/internal/e164"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

func (s *Server) smsConversations(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	convos, _ := s.store.ListSMSConversations(r.Context(), tid)
	dids, _ := s.store.ListDIDsForTenant(r.Context(), tid)
	s.renderLayout(w, r, tenant.Name+" · Messages", "sms", map[string]any{
		"Tenant":        tenant,
		"Conversations": convos,
		"DIDs":          dids,
		"NavActive":     "sms",
	})
}

func (s *Server) smsThread(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	our := strings.TrimSpace(r.URL.Query().Get("our"))
	peer := strings.TrimSpace(r.URL.Query().Get("peer"))
	if our == "" || peer == "" {
		http.Redirect(w, r, "/admin/tenants/"+tid.String()+"/sms", http.StatusSeeOther)
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	msgs, _ := s.store.ListSMSThread(r.Context(), tid, our, peer)
	s.renderLayout(w, r, tenant.Name+" · "+peer, "sms_thread", map[string]any{
		"Tenant":   tenant,
		"Our":      our,
		"Peer":     peer,
		"Messages": msgs,
	})
}

func (s *Server) smsSend(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	our := normalizeE164OrRaw(r.FormValue("our"))
	peer := normalizeE164OrRaw(r.FormValue("peer"))
	body := strings.TrimSpace(r.FormValue("body"))
	base := "/admin/tenants/" + tid.String() + "/sms"
	if our == "" || peer == "" || body == "" {
		s.flashErr(w, r, base, errors.New("pick your number, the recipient, and a message"))
		return
	}
	m, err := s.store.CreateSMS(r.Context(), store.CreateSMSInput{
		TenantID: tid, OurE164: our, PeerE164: peer,
		Direction: "outbound", Body: body, Status: "queued",
	})
	if err != nil {
		s.flashErr(w, r, base, err)
		return
	}
	// Delivery transport (SIP MESSAGE via the carrier) isn't wired yet, so the
	// message stays "queued". When the SMS trunk lands, an outbound sender will
	// pick up queued rows and flip them to sent/delivered/failed.
	ip, ua := audit.FromRequest(r)
	var actorTok *uuid.UUID
	if tok := tokenFromCtx(r.Context()); tok != nil {
		actorTok = &tok.ID
	}
	s.audit.Log(r.Context(), audit.Event{
		TenantID: &tid, ActorTokenID: actorTok,
		Event: "sms.queued", TargetType: "sms_message", TargetID: &m.ID,
		IPAddress: ip, UserAgent: ua,
		Payload: map[string]any{"our": our, "peer": peer},
	})
	http.Redirect(w, r, base+"/thread?our="+our+"&peer="+peer, http.StatusSeeOther)
}

func normalizeE164OrRaw(s string) string {
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
