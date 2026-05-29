package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type createDeviceReq struct {
	MAC    string `json:"mac"`
	Vendor string `json:"vendor"`
	Model  string `json:"model"`
	Label  string `json:"label"`
}

func (s *Server) createDevice(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	var req createDeviceReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.MAC == "" || req.Vendor == "" || req.Model == "" {
		writeErr(w, http.StatusBadRequest, "mac, vendor, model required")
		return
	}
	d, err := s.store.CreateDevice(r.Context(), tid, req.MAC, req.Vendor, req.Model, req.Label)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Task #10: push MAC to manufacturer RPS in the background. The device
	// is in our DB regardless; sync failures land in rps_last_error and the
	// admin can retry later.
	if s.rps != nil {
		// Look up tenant slug for audit/logging context.
		tenant, _ := s.store.GetTenant(r.Context(), d.TenantID)
		slug := ""
		if tenant != nil {
			slug = tenant.Slug
		}
		go func(mac, vendor, model string) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			provURL := "https://" + s.provisioningPublicHost + "/"
			if err := s.rps.Register(ctx, vendor, model, mac, provURL, slug); err != nil {
				slog.Warn("rps register failed", "mac", mac, "vendor", vendor, "err", err)
				if mErr := s.store.MarkRPSError(ctx, mac, err.Error()); mErr != nil {
					slog.Error("rps mark error", "mac", mac, "err", mErr)
				}
				return
			}
			if err := s.store.MarkRPSSynced(ctx, mac); err != nil {
				slog.Error("rps mark synced", "mac", mac, "err", err)
			}
		}(d.MAC, d.Vendor, d.Model)
	}

	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) getDevice(w http.ResponseWriter, r *http.Request) {
	mac := chi.URLParam(r, "mac")
	d, err := s.store.GetDevice(r.Context(), mac)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, d)
}
