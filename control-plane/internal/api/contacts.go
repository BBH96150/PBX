package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// listContacts returns a tenant's directory contacts (read API for integrations).
func (s *Server) listContacts(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	contacts, err := s.store.ListContactsForTenant(r.Context(), tid, r.URL.Query().Get("q"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, contacts)
}
