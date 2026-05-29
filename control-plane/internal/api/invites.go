package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

type createInviteReq struct {
	Email string `json:"email"`
	Role  string `json:"role"` // user | tenant_admin
}

func (s *Server) createInvite(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	var req createInviteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Email == "" {
		writeErr(w, http.StatusBadRequest, "email required")
		return
	}
	var inviter *uuid.UUID
	// Best-effort: stamp the inviter user. We don't have user_id on the
	// token directly; portal sessions encode email in token.name.
	// For Wave 4.5 minimum, leave nil — API tokens have no user attached.

	inv, err := s.store.CreateInvite(r.Context(), store.CreateInviteInput{
		TenantID:  tid,
		Email:     req.Email,
		Role:      req.Role,
		InvitedBy: inviter,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Send the email. Best-effort — if SMTP is misconfigured, the API
	// still returns the plaintext token so the admin can hand-deliver it.
	if s.mailer != nil {
		tenant, _ := s.store.GetTenant(r.Context(), tid)
		acceptURL := s.portalBaseURL + "/admin/accept-invite/" + inv.Plaintext
		go func() {
			tn := ""
			if tenant != nil {
				tn = tenant.Name
			}
			// Fresh ctx; request ctx is cancelled by send time.
			_, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.mailer.SendInvite(req.Email, "", tn, acceptURL); err != nil {
				slog.Warn("invite email failed", "to", req.Email, "err", err)
			}
		}()
	}

	writeJSON(w, http.StatusCreated, inv)
}

func (s *Server) listInvites(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	invites, err := s.store.ListInvitesForTenant(r.Context(), tid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, invites)
}

func (s *Server) revokeInvite(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "inviteID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid invite id")
		return
	}
	if err := s.store.RevokeInvite(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type acceptInviteReq struct {
	Token       string `json:"token"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}

func (s *Server) acceptInvite(w http.ResponseWriter, r *http.Request) {
	var req acceptInviteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	u, err := s.store.AcceptInvite(r.Context(), req.Token, req.DisplayName, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrInvalidInviteToken):
			writeErr(w, http.StatusUnauthorized, "invalid invite token")
		case errors.Is(err, store.ErrInviteExpired):
			writeErr(w, http.StatusGone, "invite expired")
		case errors.Is(err, store.ErrInviteUsed):
			writeErr(w, http.StatusConflict, "invite already used")
		case errors.Is(err, store.ErrInviteRevoked):
			writeErr(w, http.StatusGone, "invite revoked")
		default:
			writeErr(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, u)
}
