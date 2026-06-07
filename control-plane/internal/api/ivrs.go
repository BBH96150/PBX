package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/e164"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

type createIVRReq struct {
	Name                string `json:"name"`
	Extension           string `json:"extension"`
	GreetingLong        string `json:"greeting_long"`
	GreetingShort       string `json:"greeting_short"`
	InvalidSound        string `json:"invalid_sound"`
	ExitSound           string `json:"exit_sound"`
	TimeoutMS           int    `json:"timeout_ms"`
	InterDigitTimeoutMS int    `json:"inter_digit_timeout_ms"`
	MaxFailures         int    `json:"max_failures"`
	MaxTimeouts         int    `json:"max_timeouts"`
	DigitLen            int    `json:"digit_len"`
}

func (s *Server) createIVR(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	var req createIVRReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name required")
		return
	}
	v, err := s.store.CreateIVR(r.Context(), store.CreateIVRInput{
		TenantID:            tid,
		Name:                req.Name,
		Extension:           req.Extension,
		GreetingLong:        req.GreetingLong,
		GreetingShort:       req.GreetingShort,
		InvalidSound:        req.InvalidSound,
		ExitSound:           req.ExitSound,
		TimeoutMS:           req.TimeoutMS,
		InterDigitTimeoutMS: req.InterDigitTimeoutMS,
		MaxFailures:         req.MaxFailures,
		MaxTimeouts:         req.MaxTimeouts,
		DigitLen:            req.DigitLen,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

type addIVROptionReq struct {
	Digit      string     `json:"digit"`
	Label      string     `json:"label"`
	ActionKind string     `json:"action_kind"`
	ActionID   *uuid.UUID `json:"action_id"`
	ActionData string     `json:"action_data"`
}

func (s *Server) addIVROption(w http.ResponseWriter, r *http.Request) {
	ivrID, err := uuid.Parse(chi.URLParam(r, "ivrID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid ivr id")
		return
	}
	var req addIVROptionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Digit == "" || req.ActionKind == "" {
		writeErr(w, http.StatusBadRequest, "digit and action_kind required")
		return
	}
	// Wave 3.5: for dial_e164, normalize action_data at write time so the
	// menu renderer doesn't need to know about parsing rules.
	if req.ActionKind == "dial_e164" {
		if req.ActionData == "" {
			writeErr(w, http.StatusBadRequest, "action_data required for dial_e164")
			return
		}
		normalized, err := e164.Normalize(req.ActionData, "US")
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid action_data e164: "+err.Error())
			return
		}
		req.ActionData = normalized
	}
	o, err := s.store.AddIVROption(r.Context(), store.AddIVROptionInput{
		IVRID:      ivrID,
		Digit:      req.Digit,
		Label:      req.Label,
		ActionKind: req.ActionKind,
		ActionID:   req.ActionID,
		ActionData: req.ActionData,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, o)
}
