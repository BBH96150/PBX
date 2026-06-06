package portal

import (
	"net/http"
	"strings"
)

// tenantAlertEmailUpdate sets the per-tenant operational-alert recipient
// override (trunk down/up). Empty clears it (falls back to tenant admins, then
// the global ALERT_EMAIL).
func (s *Server) tenantAlertEmailUpdate(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/trunks"
	email := strings.TrimSpace(r.FormValue("alert_email"))
	if email != "" && !strings.Contains(email, "@") {
		http.Redirect(w, r, redirect+"?err=Enter+a+valid+email+or+leave+blank", http.StatusSeeOther)
		return
	}
	if err := s.store.UpdateTenantAlertEmail(r.Context(), tid, email); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "tenant.alert_email.set", "tenant", &tid, map[string]any{"set": email != ""})
	http.Redirect(w, r, redirect+"?flash=Alert+recipient+updated.", http.StatusSeeOther)
}
