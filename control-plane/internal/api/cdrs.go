package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// listCDRs returns a tenant's recent call records (read API for integrations).
// Query params: limit (default 100, max 10000), since/until (RFC3339),
// direction (inbound|outbound|internal), q (search).
func (s *Server) listCDRs(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	q := r.URL.Query()
	f := store.CDRFilter{
		Direction: q.Get("direction"),
		Search:    q.Get("q"),
		Limit:     100,
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.Limit = n
		}
	}
	if v := q.Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Since = &t
		}
	}
	if v := q.Get("until"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Until = &t
		}
	}
	cdrs, err := s.store.ListCDRsFilteredForTenant(r.Context(), tid, f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cdrs)
}
