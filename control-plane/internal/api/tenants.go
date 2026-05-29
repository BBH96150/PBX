package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type createTenantReq struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

func (s *Server) createTenant(w http.ResponseWriter, r *http.Request) {
	var req createTenantReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Slug == "" || req.Name == "" {
		writeErr(w, http.StatusBadRequest, "slug and name required")
		return
	}
	t, err := s.store.CreateTenant(r.Context(), req.Slug, req.Name)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) listTenants(w http.ResponseWriter, r *http.Request) {
	ts, err := s.store.ListTenants(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ts)
}

func (s *Server) getTenant(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	t, err := s.store.GetTenant(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

type createSIPDomainReq struct {
	Domain    string `json:"domain"`
	IsPrimary bool   `json:"is_primary"`
}

func (s *Server) createSIPDomain(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	var req createSIPDomainReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Domain == "" {
		writeErr(w, http.StatusBadRequest, "domain required")
		return
	}
	d, err := s.store.CreateSIPDomain(r.Context(), tid, req.Domain, req.IsPrimary)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, d)
}
