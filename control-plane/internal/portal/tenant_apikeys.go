package portal

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

func (s *Server) tenantAPIKeysList(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	tokens, _ := s.store.ListAPITokensForTenant(r.Context(), tid)
	s.renderLayout(w, r, tenant.Name+" · API keys", "tenant_apikeys", map[string]any{
		"Tenant":    tenant,
		"NavActive": "apikeys",
		"Tokens":    tokens,
		"NewToken":  r.URL.Query().Get("new"),
	})
}

func (s *Server) tenantAPIKeyCreate(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/api-keys"
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Redirect(w, r, redirect+"?err=Name+is+required", http.StatusSeeOther)
		return
	}
	scope := r.FormValue("scope")
	switch scope {
	case "read", "write", "admin": // ok
	default:
		scope = "read"
	}
	issued, err := s.store.CreateAPIToken(r.Context(), store.CreateAPITokenInput{
		TenantID: &tid, Name: name, Scope: scope,
	})
	if err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "api_token.created", "api_token", &issued.ID, map[string]any{"name": name, "scope": scope})
	// Show the plaintext exactly once via the ?new= param.
	http.Redirect(w, r, redirect+"?new="+issued.Plaintext, http.StatusSeeOther)
}

func (s *Server) tenantAPIKeyRevoke(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/api-keys"
	if err := s.store.RevokeAPITokenForTenant(r.Context(), tid, id); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "api_token.revoked", "api_token", &id, nil)
	http.Redirect(w, r, redirect+"?flash=API+key+revoked.", http.StatusSeeOther)
}
