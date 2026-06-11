package portal

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// dispositionCodeList renders the Disposition codes management page: the tenant's
// call wrap-up codes (CRUD). Mirrors the blocked-numbers page shape.
func (s *Server) dispositionCodeList(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	codes, _ := s.store.ListDispositionCodesForTenant(r.Context(), tid)

	data := map[string]any{
		"Tenant":    tenant,
		"NavActive": "dispositions",
		"Codes":     codes,
	}
	s.renderLayout(w, r, tenant.Name+" · Disposition codes", "disposition_codes", data)
}

func (s *Server) dispositionCodeCreate(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/disposition-codes"
	label := strings.TrimSpace(r.FormValue("label"))
	if label == "" {
		http.Redirect(w, r, redirect+"?err=A+label+is+required", http.StatusSeeOther)
		return
	}
	sortOrder, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("sort_order")))
	code, err := s.store.CreateDispositionCode(r.Context(), store.CreateDispositionCodeInput{
		TenantID:  tid,
		Label:     label,
		Color:     strings.TrimSpace(r.FormValue("color")),
		SortOrder: sortOrder,
	})
	if err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "disposition_code.created", "disposition_code", &code.ID, map[string]any{"label": label})
	http.Redirect(w, r, redirect+"?flash=Disposition+code+added.", http.StatusSeeOther)
}

func (s *Server) dispositionCodeToggle(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/disposition-codes"
	active := r.FormValue("active") == "true"
	if err := s.store.SetDispositionCodeActive(r.Context(), tid, id, active); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	msg := "Disposition+code+retired."
	if active {
		msg = "Disposition+code+activated."
	}
	http.Redirect(w, r, redirect+"?flash="+msg, http.StatusSeeOther)
}

func (s *Server) dispositionCodeDelete(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/disposition-codes"
	if err := s.store.DeleteDispositionCodeForTenant(r.Context(), tid, id); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "disposition_code.deleted", "disposition_code", &id, nil)
	http.Redirect(w, r, redirect+"?flash=Disposition+code+deleted.", http.StatusSeeOther)
}
