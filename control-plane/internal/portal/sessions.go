package portal

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/audit"
)

// ---------------------------------------------------------------------------
// /admin/security/sessions — list + revoke active portal sessions.
// ---------------------------------------------------------------------------

func (s *Server) sessionsList(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}
	sessions, _ := s.store.ListPortalSessionsForUser(r.Context(), user.Email)
	currentTok := tokenFromCtx(r.Context())
	var currentID *uuid.UUID
	if currentTok != nil {
		id := currentTok.ID
		currentID = &id
	}
	s.renderLayout(w, r, "Active sessions", "sessions", map[string]any{
		"User":           user,
		"Sessions":       sessions,
		"CurrentTokenID": currentID,
	})
}

func (s *Server) sessionRevoke(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "tokenID"))
	if err != nil {
		http.Error(w, "bad token id", 400)
		return
	}
	// Confirm this token belongs to this user before deleting (defense in
	// depth — a user can't revoke someone else's session).
	mine, _ := s.store.ListPortalSessionsForUser(r.Context(), user.Email)
	owned := false
	for _, t := range mine {
		if t.ID == id {
			owned = true
			break
		}
	}
	if !owned {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := s.store.RevokeAPIToken(r.Context(), id); err != nil {
		s.errPage(w, r, err)
		return
	}
	ip, ua := audit.FromRequest(r)
	s.audit.Log(r.Context(), audit.Event{
		ActorUserID: &user.ID, ActorEmail: user.Email,
		Event: "session.revoke", IPAddress: ip, UserAgent: ua,
		TargetType: "api_token", TargetID: &id,
	})
	http.Redirect(w, r, "/admin/security/sessions?flash=Session+revoked.", http.StatusSeeOther)
}

func (s *Server) sessionRevokeAll(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}
	// Preserve the caller's current token so they aren't immediately bounced
	// to the login page.
	var keepID *uuid.UUID
	if tok := tokenFromCtx(r.Context()); tok != nil {
		id := tok.ID
		keepID = &id
	}
	n, err := s.store.RevokeAllPortalSessionsForUser(r.Context(), user.Email, keepID)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	ip, ua := audit.FromRequest(r)
	s.audit.Log(r.Context(), audit.Event{
		ActorUserID: &user.ID, ActorEmail: user.Email,
		Event: "session.revoke_all", IPAddress: ip, UserAgent: ua,
		Payload: map[string]any{"revoked": n},
	})
	http.Redirect(w, r,
		"/admin/security/sessions?flash=Signed+out+of+other+sessions.",
		http.StatusSeeOther)
}
