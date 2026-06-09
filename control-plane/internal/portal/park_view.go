package portal

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// parkList renders the Call park page: all of a tenant's park lots. Mirrors the
// conference page shape.
func (s *Server) parkList(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	lots, _ := s.store.ListParkLotsForTenant(r.Context(), tid)

	data := map[string]any{
		"Tenant":    tenant,
		"NavActive": "park",
		"Lots":      lots,
	}
	s.renderLayout(w, r, tenant.Name+" · Call park", "park", data)
}

func (s *Server) parkCreate(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/park"
	name := strings.TrimSpace(r.FormValue("name"))
	code := strings.TrimSpace(r.FormValue("feature_code"))
	if name == "" || code == "" {
		http.Redirect(w, r, redirect+"?err=Name+and+feature+code+are+required", http.StatusSeeOther)
		return
	}
	slotStart, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("slot_start")))
	slotEnd, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("slot_end")))
	if slotEnd < slotStart {
		http.Redirect(w, r, redirect+"?err=Slot+end+must+be+%E2%89%A5+slot+start", http.StatusSeeOther)
		return
	}
	lot, err := s.store.CreateParkLot(r.Context(), store.CreateParkLotInput{
		TenantID:    tid,
		Name:        name,
		FeatureCode: code,
		SlotStart:   slotStart,
		SlotEnd:     slotEnd,
	})
	if err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "park_lot.created", "park_lot", &lot.ID, map[string]any{"name": name, "feature_code": code})
	http.Redirect(w, r, redirect+"?flash=Park+lot+created.", http.StatusSeeOther)
}

func (s *Server) parkDelete(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/park"
	if err := s.store.DeleteParkLotForTenant(r.Context(), tid, id); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "park_lot.deleted", "park_lot", &id, nil)
	http.Redirect(w, r, redirect+"?flash=Park+lot+deleted.", http.StatusSeeOther)
}

func (s *Server) parkToggle(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/park"
	enabled := r.FormValue("enabled") == "true"
	if err := s.store.SetParkLotEnabled(r.Context(), tid, id, enabled); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "park_lot.toggled", "park_lot", &id, map[string]any{"enabled": enabled})
	http.Redirect(w, r, redirect+"?flash=Updated.", http.StatusSeeOther)
}
