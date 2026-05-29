package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

type createDeviceLineReq struct {
	LineNumber  int       `json:"line_number"`
	ExtensionID uuid.UUID `json:"extension_id"`
	Label       string    `json:"label"`
}

func (s *Server) createDeviceLine(w http.ResponseWriter, r *http.Request) {
	mac := chi.URLParam(r, "mac")
	var req createDeviceLineReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.LineNumber < 1 || req.ExtensionID == uuid.Nil {
		writeErr(w, http.StatusBadRequest, "line_number (>=1) and extension_id required")
		return
	}
	dl, err := s.store.CreateDeviceLine(r.Context(), mac, req.LineNumber, req.ExtensionID, req.Label)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrCrossTenant):
			writeErr(w, http.StatusBadRequest, err.Error())
		default:
			writeErr(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusCreated, dl)
}
