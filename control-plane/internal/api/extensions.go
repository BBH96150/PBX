package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// listExtensions returns a tenant's active extensions (read API for integrations).
func (s *Server) listExtensions(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	exts, err := s.store.ListExtensionsForTenant(r.Context(), tid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, exts)
}

type createExtensionReq struct {
	SIPDomainID uuid.UUID `json:"sip_domain_id"`
	Extension   string    `json:"extension"`
	SIPUsername string    `json:"sip_username"`
	SIPPassword string    `json:"sip_password"`
	DisplayName string    `json:"display_name"`
}

func (s *Server) createExtension(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	var req createExtensionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Extension == "" || req.SIPDomainID == uuid.Nil {
		writeErr(w, http.StatusBadRequest, "extension and sip_domain_id required")
		return
	}
	e, err := s.store.CreateExtension(
		r.Context(), tid, req.SIPDomainID,
		req.Extension, req.SIPUsername, req.SIPPassword, req.DisplayName,
	)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

// PATCH-style update for the Phase 3 Wave 5.0 feature flags. Pointer fields
// distinguish "set to this value" from "leave unchanged" — important for
// booleans where the zero value (false) is a real possible target value.
type updateExtensionFeaturesReq struct {
	DoNotDisturb     *bool   `json:"do_not_disturb"`
	CFImmediate      *string `json:"cf_immediate"`
	CFBusy           *string `json:"cf_busy"`
	CFNoAnswer       *string `json:"cf_no_answer"`
	VoicemailEnabled *bool   `json:"voicemail_enabled"`
	RecordingEnabled *bool   `json:"recording_enabled"`
}

func (s *Server) updateExtensionFeatures(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "extensionID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid extension id")
		return
	}
	var req updateExtensionFeaturesReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	e, err := s.store.UpdateExtensionFeatures(r.Context(), id, store.UpdateExtensionFeaturesInput{
		DoNotDisturb:     req.DoNotDisturb,
		CFImmediate:      req.CFImmediate,
		CFBusy:           req.CFBusy,
		CFNoAnswer:       req.CFNoAnswer,
		VoicemailEnabled: req.VoicemailEnabled,
		RecordingEnabled: req.RecordingEnabled,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, e)
}
