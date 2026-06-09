package portal

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// e911List renders the E911 / Locations page: a tenant's dispatchable civic
// addresses plus an extension→location assignment table. Mirrors the conference
// page shape; lives under the Calling ▾ subnav.
func (s *Server) e911List(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	locations, _ := s.store.ListE911LocationsForTenant(r.Context(), tid)
	extensions, _ := s.store.ListExtensionsForTenant(r.Context(), tid)

	data := map[string]any{
		"Tenant":     tenant,
		"NavActive":  "e911",
		"Locations":  locations,
		"Extensions": extensions,
	}
	s.renderLayout(w, r, tenant.Name+" · E911 / Locations", "e911", data)
}

func (s *Server) e911Create(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/e911"
	label := strings.TrimSpace(r.FormValue("label"))
	street := strings.TrimSpace(r.FormValue("street"))
	city := strings.TrimSpace(r.FormValue("city"))
	region := strings.TrimSpace(r.FormValue("region"))
	postal := strings.TrimSpace(r.FormValue("postal_code"))
	if label == "" || street == "" || city == "" || region == "" || postal == "" {
		http.Redirect(w, r, redirect+"?err=Label%2C+street%2C+city%2C+state+and+ZIP+are+required", http.StatusSeeOther)
		return
	}
	country := strings.TrimSpace(r.FormValue("country"))
	if country == "" {
		country = "US"
	}
	loc, err := s.store.CreateE911Location(r.Context(), store.CreateE911LocationInput{
		TenantID:   tid,
		Label:      label,
		Street:     street,
		Street2:    strings.TrimSpace(r.FormValue("street2")),
		City:       city,
		Region:     region,
		PostalCode: postal,
		Country:    country,
	})
	if err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "e911_location.created", "e911_location", &loc.ID, map[string]any{"label": label})
	http.Redirect(w, r, redirect+"?flash=Location+created.", http.StatusSeeOther)
}

func (s *Server) e911Delete(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/e911"
	if err := s.store.DeleteE911LocationForTenant(r.Context(), tid, id); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "e911_location.deleted", "e911_location", &id, nil)
	http.Redirect(w, r, redirect+"?flash=Location+deleted.", http.StatusSeeOther)
}

func (s *Server) e911Toggle(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/e911"
	enabled := r.FormValue("enabled") == "true"
	if err := s.store.SetE911LocationEnabled(r.Context(), tid, id, enabled); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "e911_location.toggled", "e911_location", &id, map[string]any{"enabled": enabled})
	http.Redirect(w, r, redirect+"?flash=Updated.", http.StatusSeeOther)
}

// e911Assign assigns (or clears) an extension's dispatchable location. An empty
// location_id detaches the extension.
func (s *Server) e911Assign(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/e911"
	extID, err := uuid.Parse(strings.TrimSpace(r.FormValue("extension_id")))
	if err != nil {
		http.Redirect(w, r, redirect+"?err=Invalid+extension", http.StatusSeeOther)
		return
	}
	var locPtr *uuid.UUID
	if raw := strings.TrimSpace(r.FormValue("location_id")); raw != "" {
		locID, perr := uuid.Parse(raw)
		if perr != nil {
			http.Redirect(w, r, redirect+"?err=Invalid+location", http.StatusSeeOther)
			return
		}
		locPtr = &locID
	}
	if err := s.store.SetExtensionE911Location(r.Context(), tid, extID, locPtr); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "e911_location.assigned", "extension", &extID, map[string]any{"location_id": r.FormValue("location_id")})
	http.Redirect(w, r, redirect+"?flash=Assignment+updated.", http.StatusSeeOther)
}
