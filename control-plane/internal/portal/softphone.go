package portal

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/audit"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// softphonePageData powers the /admin/softphone page. The JS half pulls
// fresh creds via POST /admin/softphone/credentials on user click.
type softphonePageData struct {
	OwnedExtensions []store.Extension
	WSURL           string // ws:// or wss:// URL to Kamailio's WebSocket listener
	Realm           string // SIP realm — defaults to extension's domain
}

// /admin/softphone — GET. Renders the SIP.js-based softphone UI.
func (s *Server) softphoneGet(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}
	exts, _ := s.store.FindOwnedExtensions(r.Context(), user.ID)
	data := softphonePageData{
		OwnedExtensions: exts,
		WSURL:           deriveWebSocketURL(s.portalBaseURL),
	}
	s.renderLayout(w, r, "Web softphone", "softphone", map[string]any{
		"User": user,
		"Data": data,
	})
}

// /admin/softphone/credentials — POST. Rotates webphone password for the
// caller's chosen extension, returns JSON with the plaintext credential
// bundle. The browser hands these directly to SIP.js for REGISTER.
func (s *Server) softphoneCredentialsPost(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	extID, err := uuid.Parse(strings.TrimSpace(r.FormValue("extension_id")))
	if err != nil {
		http.Error(w, "extension_id required", 400)
		return
	}
	creds, err := s.store.IssueWebphoneCredentials(r.Context(), user.ID, extID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	ip, ua := audit.FromRequest(r)
	s.audit.Log(r.Context(), audit.Event{
		ActorUserID: &user.ID, ActorEmail: user.Email,
		Event: "webphone.credentials.issued",
		TargetType: "extension", TargetID: &extID,
		IPAddress: ip, UserAgent: ua,
		Payload: map[string]any{"sip_username": creds.Username},
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(creds)
}

// deriveWebSocketURL turns the portal HTTP base URL into the WS URL the
// browser uses to reach Kamailio. The control-plane and Kamailio are
// independent processes; in OrbStack dev they're both reachable from the
// host on different ports. Here we just swap scheme and use the same host
// at Kamailio's documented WS port (5066).
//
// In production this would come from config (KAMAILIO_WS_URL env or similar).
// For now we derive it; the operator can override via env later if needed.
func deriveWebSocketURL(portalBaseURL string) string {
	if portalBaseURL == "" {
		return "ws://127.0.0.1:5066"
	}
	if strings.HasPrefix(portalBaseURL, "https://") {
		host := strings.TrimPrefix(portalBaseURL, "https://")
		host = stripPort(host)
		return "wss://" + host + ":5066"
	}
	host := strings.TrimPrefix(portalBaseURL, "http://")
	host = stripPort(host)
	return "ws://" + host + ":5066"
}

func stripPort(hostport string) string {
	if i := strings.IndexByte(hostport, '/'); i >= 0 {
		hostport = hostport[:i]
	}
	if i := strings.LastIndexByte(hostport, ':'); i >= 0 {
		return hostport[:i]
	}
	return hostport
}

