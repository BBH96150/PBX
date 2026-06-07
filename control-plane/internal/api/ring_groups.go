package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

type createRingGroupReq struct {
	Extension      string     `json:"extension"`
	Name           string     `json:"name"`
	Strategy       string     `json:"strategy"`         // simultaneous|sequential|round_robin|random
	RingTimeoutSec int        `json:"ring_timeout_sec"` // default 30
	FallbackKind   string     `json:"fallback_kind"`    // extension|ring_group|voicemail|ivr|hangup
	FallbackID     *uuid.UUID `json:"fallback_id"`
	CallerIDPrefix string     `json:"caller_id_prefix"`
}

func (s *Server) createRingGroup(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	var req createRingGroupReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name required")
		return
	}
	rg, err := s.store.CreateRingGroup(r.Context(), store.CreateRingGroupInput{
		TenantID:       tid,
		Extension:      req.Extension,
		Name:           req.Name,
		Strategy:       req.Strategy,
		RingTimeoutSec: req.RingTimeoutSec,
		FallbackKind:   req.FallbackKind,
		FallbackID:     req.FallbackID,
		CallerIDPrefix: req.CallerIDPrefix,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, rg)
}

func (s *Server) listRingGroups(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	groups, err := s.store.ListRingGroupsForTenant(r.Context(), tid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, groups)
}

type addRingGroupMemberReq struct {
	ExtensionID  uuid.UUID `json:"extension_id"`
	Priority     int       `json:"priority"`
	RingDelaySec int       `json:"ring_delay_sec"`
}

func (s *Server) addRingGroupMember(w http.ResponseWriter, r *http.Request) {
	rgID, err := uuid.Parse(chi.URLParam(r, "ringGroupID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid ring group id")
		return
	}
	var req addRingGroupMemberReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.ExtensionID == uuid.Nil {
		writeErr(w, http.StatusBadRequest, "extension_id required")
		return
	}
	m, err := s.store.AddRingGroupMember(r.Context(), store.AddRingGroupMemberInput{
		RingGroupID:  rgID,
		ExtensionID:  req.ExtensionID,
		Priority:     req.Priority,
		RingDelaySec: req.RingDelaySec,
	})
	if err != nil {
		switch {
		case errors.Is(err, store.ErrCrossTenant):
			writeErr(w, http.StatusBadRequest, err.Error())
		default:
			writeErr(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusCreated, m)
}
