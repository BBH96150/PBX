package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

type requestResetReq struct {
	Email string `json:"email"`
}

// requestPasswordReset is deliberately opaque: it always returns 202 so an
// attacker can't enumerate which emails exist on the platform. Mail send
// runs in the background.
func (s *Server) requestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req requestResetReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Email == "" {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	go func(email string) {
		// Fresh context: the request's ctx is cancelled by the time we run.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		issued, err := s.store.CreatePasswordResetToken(ctx, email)
		if err != nil {
			slog.Debug("password reset request for unknown email", "email", email, "err", err)
			return
		}
		if s.mailer == nil {
			return
		}
		url := s.portalBaseURL + "/admin/reset-password/" + issued.Plaintext
		if err := s.mailer.SendPasswordReset(email, url); err != nil {
			slog.Warn("reset email failed", "to", email, "err", err)
		}
	}(req.Email)

	w.WriteHeader(http.StatusAccepted)
}

type confirmResetReq struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

func (s *Server) confirmPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req confirmResetReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	u, err := s.store.ConsumeResetToken(r.Context(), req.Token, req.NewPassword)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrInvalidResetToken):
			writeErr(w, http.StatusUnauthorized, "invalid token")
		case errors.Is(err, store.ErrResetTokenExpired):
			writeErr(w, http.StatusGone, "token expired")
		case errors.Is(err, store.ErrResetTokenUsed):
			writeErr(w, http.StatusConflict, "token already used")
		default:
			writeErr(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, u)
}
