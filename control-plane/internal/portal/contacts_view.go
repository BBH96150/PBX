package portal

import (
	"encoding/csv"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (s *Server) contactsList(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	contacts, _ := s.store.ListContactsForTenant(r.Context(), tid, search)
	s.renderLayout(w, r, tenant.Name+" · Directory", "contacts", map[string]any{
		"Tenant":    tenant,
		"NavActive": "contacts",
		"Contacts":  contacts,
		"Search":    search,
	})
}

func (s *Server) contactsCSV(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if _, err := s.store.GetTenant(r.Context(), tid); err != nil {
		s.errPage(w, r, err)
		return
	}
	contacts, _ := s.store.ListContactsForTenant(r.Context(), tid, strings.TrimSpace(r.URL.Query().Get("q")))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="directory.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"name", "number", "company", "notes"})
	for _, c := range contacts {
		_ = cw.Write([]string{csvSafe(c.Name), csvSafe(c.Number), csvSafe(c.Company), csvSafe(c.Notes)})
	}
	cw.Flush()
}

func (s *Server) contactCreate(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/contacts"
	name := strings.TrimSpace(r.FormValue("name"))
	number := strings.TrimSpace(r.FormValue("number"))
	if name == "" || number == "" {
		http.Redirect(w, r, redirect+"?err=Name+and+number+are+required", http.StatusSeeOther)
		return
	}
	c, err := s.store.CreateContact(r.Context(), tid, name, number,
		strings.TrimSpace(r.FormValue("company")), strings.TrimSpace(r.FormValue("notes")))
	if err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "contact.created", "contact", &c.ID, map[string]any{"name": name})
	http.Redirect(w, r, redirect+"?flash=Contact+added.", http.StatusSeeOther)
}

func (s *Server) contactDelete(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/contacts"
	if err := s.store.DeleteContactForTenant(r.Context(), tid, id); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "contact.deleted", "contact", &id, nil)
	http.Redirect(w, r, redirect+"?flash=Contact+removed.", http.StatusSeeOther)
}
