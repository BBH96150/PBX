package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

type createAPITokenReq struct {
	TenantID  *uuid.UUID `json:"tenant_id,omitempty"` // nil → super-admin
	Name      string     `json:"name"`
	Scope     string     `json:"scope"`     // read|write|admin (default write)
	ExpiresIn string     `json:"expires_in,omitempty"` // duration string, e.g. "720h"; optional
}

func (s *Server) createAPIToken(w http.ResponseWriter, r *http.Request) {
	var req createAPITokenReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name required")
		return
	}
	in := store.CreateAPITokenInput{
		TenantID: req.TenantID,
		Name:     req.Name,
		Scope:    req.Scope,
	}
	if req.ExpiresIn != "" {
		d, err := time.ParseDuration(req.ExpiresIn)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid expires_in (e.g. 720h, 30d): "+err.Error())
			return
		}
		t := time.Now().Add(d)
		in.ExpiresAt = &t
	}
	issued, err := s.store.CreateAPIToken(r.Context(), in)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, issued)
}

func (s *Server) listAPITokens(w http.ResponseWriter, r *http.Request) {
	ts, err := s.store.ListAPITokens(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ts)
}

func (s *Server) revokeAPIToken(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "tokenID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid token id")
		return
	}
	if err := s.store.RevokeAPIToken(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
