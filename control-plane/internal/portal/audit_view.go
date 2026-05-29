package portal

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/audit"
)

func (s *Server) auditList(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	entries, _ := s.store.ListAuditForTenant(r.Context(), tid, 200)
	requireVerified, _ := s.store.TenantRequiresEmailVerified(r.Context(), tid)
	require2FA, _ := s.store.TenantRequires2FA(r.Context(), tid)
	s.renderLayout(w, r, tenant.Name+" · Audit log", "audit", map[string]any{
		"Tenant":          tenant,
		"Entries":         entries,
		"RequireVerified": requireVerified,
		"Require2FA":      require2FA,
	})
}

func (s *Server) tenantSecurityUpdate(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	requireVerified := r.FormValue("require_email_verified") == "true"
	require2FA := r.FormValue("require_2fa") == "true"
	if _, err := s.store.DB.Exec(r.Context(),
		`UPDATE tenants SET require_email_verified = $2, require_2fa = $3 WHERE id = $1`,
		tid, requireVerified, require2FA,
	); err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/audit", err)
		return
	}
	ip, ua := audit.FromRequest(r)
	var actorTok *uuid.UUID
	if tok := tokenFromCtx(r.Context()); tok != nil {
		actorTok = &tok.ID
	}
	s.audit.Log(r.Context(), audit.Event{
		TenantID:     &tid,
		ActorTokenID: actorTok,
		Event:        "tenant.security.update",
		TargetType:   "tenant", TargetID: &tid,
		IPAddress: ip, UserAgent: ua,
		Payload: map[string]any{
			"require_email_verified": requireVerified,
			"require_2fa":            require2FA,
		},
	})
	http.Redirect(w, r, "/admin/tenants/"+tid.String()+"/audit?flash=updated", http.StatusSeeOther)
}
