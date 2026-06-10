package portal

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// blockedNumberList renders the Blocked numbers page: the tenant's inbound
// call-screening blocklist. Mirrors the conference page shape.
func (s *Server) blockedNumberList(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	numbers, _ := s.store.ListBlockedNumbersForTenant(r.Context(), tid)

	data := map[string]any{
		"Tenant":    tenant,
		"NavActive": "blocked",
		"Numbers":   numbers,
	}
	s.renderLayout(w, r, tenant.Name+" · Blocked numbers", "blocked_numbers", data)
}

func (s *Server) blockedNumberCreate(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/blocked-numbers"
	number := strings.TrimSpace(r.FormValue("number"))
	if number == "" {
		http.Redirect(w, r, redirect+"?err=A+phone+number+is+required", http.StatusSeeOther)
		return
	}
	bn, err := s.store.CreateBlockedNumber(r.Context(), store.CreateBlockedNumberInput{
		TenantID: tid,
		Number:   number,
		Label:    strings.TrimSpace(r.FormValue("label")),
	})
	if err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "blocked_number.created", "blocked_number", &bn.ID, map[string]any{"number": number})
	http.Redirect(w, r, redirect+"?flash=Number+blocked.", http.StatusSeeOther)
}

func (s *Server) blockedNumberDelete(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/blocked-numbers"
	if err := s.store.DeleteBlockedNumberForTenant(r.Context(), tid, id); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "blocked_number.deleted", "blocked_number", &id, nil)
	http.Redirect(w, r, redirect+"?flash=Number+unblocked.", http.StatusSeeOther)
}
