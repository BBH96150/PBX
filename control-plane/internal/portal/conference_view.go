package portal

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// conferenceList renders the Conferences page: all of a tenant's meet-me
// conference bridges. Mirrors the paging page shape.
func (s *Server) conferenceList(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	rooms, _ := s.store.ListConferenceRoomsForTenant(r.Context(), tid)

	data := map[string]any{
		"Tenant":    tenant,
		"NavActive": "conferences",
		"Rooms":     rooms,
	}
	s.renderLayout(w, r, tenant.Name+" · Conferences", "conference", data)
}

func (s *Server) conferenceCreate(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/conferences"
	name := strings.TrimSpace(r.FormValue("name"))
	ext := strings.TrimSpace(r.FormValue("extension"))
	if name == "" || ext == "" {
		http.Redirect(w, r, redirect+"?err=Name+and+room+number+are+required", http.StatusSeeOther)
		return
	}
	maxMembers := 0
	if m := strings.TrimSpace(r.FormValue("max_members")); m != "" {
		maxMembers, _ = strconv.Atoi(m)
	}
	room, err := s.store.CreateConferenceRoom(r.Context(), store.CreateConferenceRoomInput{
		TenantID:      tid,
		Extension:     ext,
		Name:          name,
		PIN:           strings.TrimSpace(r.FormValue("pin")),
		ModeratorPIN:  strings.TrimSpace(r.FormValue("moderator_pin")),
		MaxMembers:    maxMembers,
		Record:        r.FormValue("record") == "true",
		AnnounceCount: r.FormValue("announce_count") == "true",
	})
	if err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "conference_room.created", "conference_room", &room.ID, map[string]any{"name": name, "extension": ext})
	http.Redirect(w, r, redirect+"?flash=Conference+room+created.", http.StatusSeeOther)
}

func (s *Server) conferenceDelete(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/conferences"
	if err := s.store.DeleteConferenceRoomForTenant(r.Context(), tid, id); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "conference_room.deleted", "conference_room", &id, nil)
	http.Redirect(w, r, redirect+"?flash=Conference+room+deleted.", http.StatusSeeOther)
}

func (s *Server) conferenceToggle(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/conferences"
	enabled := r.FormValue("enabled") == "true"
	if err := s.store.SetConferenceRoomEnabled(r.Context(), tid, id, enabled); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "conference_room.toggled", "conference_room", &id, map[string]any{"enabled": enabled})
	http.Redirect(w, r, redirect+"?flash=Updated.", http.StatusSeeOther)
}
