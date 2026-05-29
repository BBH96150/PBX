package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

type createUserReq struct {
	TenantID         *uuid.UUID `json:"tenant_id,omitempty"` // omit for super-admin
	Email            string     `json:"email"`
	DisplayName      string     `json:"display_name"`
	Role             string     `json:"role"` // user|tenant_admin|super_admin
	Password         string     `json:"password"`
	SendVerification bool       `json:"send_verification,omitempty"` // Phase 4.9
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var req createUserReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	// Only super-admin can mint super-admin users. The middleware already
	// gated this route to admin scope; here we just refuse cross-promotion.
	if req.Role == "super_admin" {
		tok := AuthToken(r.Context())
		if tok == nil || tok.TenantID != nil {
			writeErr(w, http.StatusForbidden, "only super-admin tokens can create super-admin users")
			return
		}
	}
	u, err := s.store.CreateUser(r.Context(), store.CreateUserInput{
		TenantID:    req.TenantID,
		Email:       req.Email,
		DisplayName: req.DisplayName,
		Role:        req.Role,
		Password:    req.Password,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// Phase 4.9: opt-in verification email dispatch for admin-created users.
	if req.SendVerification {
		issued, terr := s.store.CreateEmailVerificationToken(r.Context(), u.ID, 0)
		if terr != nil {
			slog.Warn("verification token create failed", "user", u.Email, "err", terr)
		} else if s.mailer != nil && s.portalBaseURL != "" {
			verifyURL := s.portalBaseURL + "/admin/verify-email/" + issued.Plaintext
			go func(to, url string) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				_ = ctx
				if err := s.mailer.SendEmailVerification(to, url); err != nil {
					slog.Warn("verification email failed", "to", to, "err", err)
				}
			}(u.Email, verifyURL)
		}
	}
	writeJSON(w, http.StatusCreated, u)
}

type setPasswordReq struct {
	Password string `json:"password"`
}

func (s *Server) setUserPassword(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var req setPasswordReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Password == "" {
		writeErr(w, http.StatusBadRequest, "password required")
		return
	}
	if err := s.store.SetUserPassword(r.Context(), id, req.Password); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
