package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/e164"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

type createDIDReq struct {
	CarrierID        uuid.UUID  `json:"carrier_id"`
	CarrierAccountID *uuid.UUID `json:"carrier_account_id"`
	E164             string     `json:"e164"`             // raw or pre-normalized
	DestinationKind  string     `json:"destination_kind"` // "extension" (Phase 2)
	DestinationID    uuid.UUID  `json:"destination_id"`
	CNAM             string     `json:"cnam"`
}

func (s *Server) createDID(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	var req createDIDReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.CarrierID == uuid.Nil || req.E164 == "" ||
		req.DestinationKind == "" || req.DestinationID == uuid.Nil {
		writeErr(w, http.StatusBadRequest, "carrier_id, e164, destination_kind, destination_id required")
		return
	}
	// Phase 3 Wave 4: all destination_kinds wired.
	switch req.DestinationKind {
	case "extension", "ring_group", "voicemail", "ivr", "queue":
		// ok
	default:
		writeErr(w, http.StatusBadRequest,
			"destination_kind must be 'extension', 'ring_group', 'voicemail', 'ivr', or 'queue'")
		return
	}
	normalized, err := e164.Normalize(req.E164, "US")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid e164: "+err.Error())
		return
	}
	did, err := s.store.CreateDID(r.Context(), store.CreateDIDInput{
		TenantID:         tid,
		CarrierID:        req.CarrierID,
		CarrierAccountID: req.CarrierAccountID,
		E164:             normalized,
		DestinationKind:  req.DestinationKind,
		DestinationID:    req.DestinationID,
		CNAM:             req.CNAM,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, did)
}

func (s *Server) listDIDs(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	ds, err := s.store.ListDIDsForTenant(r.Context(), tid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ds)
}
