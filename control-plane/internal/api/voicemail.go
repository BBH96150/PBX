package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

type createVoicemailBoxReq struct {
	PIN               string `json:"pin"`
	Email             string `json:"email"`
	Timezone          string `json:"timezone"`
	MaxMessages       int    `json:"max_messages"`
	MaxMsgDurationSec int    `json:"max_msg_duration_sec"`
}

func (s *Server) createVoicemailBox(w http.ResponseWriter, r *http.Request) {
	extID, err := uuid.Parse(chi.URLParam(r, "extensionID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid extension id")
		return
	}
	var req createVoicemailBoxReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.PIN == "" {
		writeErr(w, http.StatusBadRequest, "pin required (4-12 digits)")
		return
	}

	// Look up the extension to get tenant_id.
	const q = `SELECT tenant_id FROM extensions WHERE id = $1`
	var tenantID uuid.UUID
	if err := s.store.DB.QueryRow(r.Context(), q, extID).Scan(&tenantID); err != nil {
		writeErr(w, http.StatusNotFound, "extension not found")
		return
	}

	box, err := s.store.CreateVoicemailBox(r.Context(), store.CreateVoicemailBoxInput{
		TenantID:          tenantID,
		ExtensionID:       extID,
		PIN:               req.PIN,
		Email:             req.Email,
		Timezone:          req.Timezone,
		MaxMessages:       req.MaxMessages,
		MaxMsgDurationSec: req.MaxMsgDurationSec,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, box)
}

func (s *Server) getVoicemailBox(w http.ResponseWriter, r *http.Request) {
	extID, err := uuid.Parse(chi.URLParam(r, "extensionID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid extension id")
		return
	}
	box, err := s.store.GetVoicemailBoxByExtensionID(r.Context(), extID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "no voicemail box for extension")
		return
	}
	writeJSON(w, http.StatusOK, box)
}
