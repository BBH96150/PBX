package portal

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/tendpos/sip-platform/control-plane/internal/audit"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// resendCooldown is the minimum gap between consecutive verification emails
// to the same user. Both /resend and the signup re-send button honor it.
const resendCooldown = 60 * time.Second

// dispatchVerificationEmail generates a fresh token and fires the email in
// a goroutine. Used by signup, resend, and the admin API path. Cooldown=0
// for the initial signup send.
func (s *Server) dispatchVerificationEmail(ctx context.Context, user *store.User, cooldown time.Duration) error {
	issued, err := s.store.CreateEmailVerificationToken(ctx, user.ID, cooldown)
	if err != nil {
		return err
	}
	verifyURL := s.portalBaseURL + "/admin/verify-email/" + issued.Plaintext
	if s.mailer != nil {
		go func(to, url string) {
			cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = cctx
			if err := s.mailer.SendEmailVerification(to, url); err != nil {
				slog.Warn("verification email failed", "to", to, "err", err)
			}
		}(user.Email, verifyURL)
	}
	// Dev convenience: when no SMTP backend is wired (mailer interface nil
	// or wrapping an unconfigured client) the email is dropped silently.
	// Log the link at INFO so an operator can paste it into a browser.
	slog.Info("email verification link issued", "user", user.Email, "url", verifyURL)
	return nil
}

// ---------------------------------------------------------------------------
// GET /admin/verify-email/{token} — public consume endpoint.
// ---------------------------------------------------------------------------

func (s *Server) verifyEmailGet(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	user, err := s.store.ConsumeEmailVerificationToken(r.Context(), token)
	if err != nil {
		msg := "This verification link is invalid."
		switch {
		case errors.Is(err, store.ErrVerificationTokenExpired):
			msg = "This verification link has expired. Request a new one from the portal."
		case errors.Is(err, store.ErrVerificationTokenUsed):
			msg = "This verification link has already been used. You're good to go."
		}
		s.render(w, "verify_email_result", map[string]any{"Flash": msg, "Bad": true})
		return
	}
	ip, ua := audit.FromRequest(r)
	s.audit.Log(r.Context(), audit.Event{
		ActorUserID: &user.ID, ActorEmail: user.Email,
		Event: audit.EventEmailVerified, IPAddress: ip, UserAgent: ua,
	})

	// If they're already signed in, bounce them to dashboard with a flash.
	if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
		if _, err := s.store.VerifyAPIToken(r.Context(), c.Value); err == nil {
			http.Redirect(w, r, "/admin/?flash=Email+verified.", http.StatusSeeOther)
			return
		}
	}
	s.render(w, "verify_email_result", map[string]any{"Email": user.Email})
}

// ---------------------------------------------------------------------------
// GET /admin/verify-email/sent — landing shown after signup or resend.
// ---------------------------------------------------------------------------

func (s *Server) verifyEmailSent(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	s.renderLayout(w, r, "Verify your email", "verify_email_sent", map[string]any{
		"Email":          email,
		"PortalConfigured": s.mailer != nil,
	})
}

// ---------------------------------------------------------------------------
// POST /admin/verify-email/resend — authenticated, rate-limited.
// ---------------------------------------------------------------------------

func (s *Server) verifyEmailResend(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}
	if user.EmailVerifiedAt != nil {
		http.Redirect(w, r, "/admin/?flash=Email+already+verified.", http.StatusSeeOther)
		return
	}
	err := s.dispatchVerificationEmail(r.Context(), user, resendCooldown)
	if err != nil {
		if errors.Is(err, store.ErrVerificationCooldown) {
			http.Redirect(w, r,
				"/admin/verify-email/sent?email="+user.Email+"&flash=Just+sent+a+verification+email+a+moment+ago.+Check+your+inbox.",
				http.StatusSeeOther)
			return
		}
		s.errPage(w, r, err)
		return
	}
	ip, ua := audit.FromRequest(r)
	s.audit.Log(r.Context(), audit.Event{
		ActorUserID: &user.ID, ActorEmail: user.Email,
		Event: "auth.email.verification.sent", IPAddress: ip, UserAgent: ua,
	})
	http.Redirect(w, r, "/admin/verify-email/sent?email="+user.Email, http.StatusSeeOther)
}
