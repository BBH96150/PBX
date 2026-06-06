package api

import (
	"encoding/json"
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

type createContactReq struct {
	Name    string `json:"name"`
	Number  string `json:"number"`
	Company string `json:"company"`
	Notes   string `json:"notes"`
}

// createContact adds a directory contact via the API.
func (s *Server) createContact(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	var req createContactReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name == "" || req.Number == "" {
		writeErr(w, http.StatusBadRequest, "name and number are required")
		return
	}
	c, err := s.store.CreateContact(r.Context(), tid, req.Name, req.Number, req.Company, req.Notes)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// deleteContact removes a directory contact via the API.
func (s *Server) deleteContact(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	cid, err := uuid.Parse(chi.URLParam(r, "contactID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid contact id")
		return
	}
	if err := s.store.DeleteContactForTenant(r.Context(), tid, cid); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
