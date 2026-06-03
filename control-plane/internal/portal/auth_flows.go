package portal

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/audit"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// ---------------------------------------------------------------------------
// Forgot / Reset password
// ---------------------------------------------------------------------------

func (s *Server) handleForgotPasswordGet(w http.ResponseWriter, r *http.Request) {
	s.render(w, "forgot_password", map[string]any{
		"Flash": r.URL.Query().Get("flash"),
	})
}

func (s *Server) handleForgotPasswordPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.render(w, "forgot_password", map[string]any{"Flash": "bad form"})
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		s.render(w, "forgot_password", map[string]any{"Flash": "Enter your email."})
		return
	}

	ip, ua := audit.FromRequest(r)
	s.audit.Log(r.Context(), audit.Event{
		ActorEmail: email, Event: audit.EventPasswordResetRequest,
		IPAddress: ip, UserAgent: ua,
	})

	// Best-effort. Always render the same confirmation, regardless of
	// whether the email matched a real user, to avoid enumeration.
	issued, err := s.store.CreatePasswordResetToken(r.Context(), email)
	if err == nil && s.mailer != nil {
		resetURL := s.portalBaseURL + "/admin/reset-password/" + issued.Plaintext
		go func(to, url string) {
			_, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.mailer.SendPasswordReset(to, url); err != nil {
				slog.Warn("password-reset email failed", "to", to, "err", err)
			}
		}(email, resetURL)
	} else if err != nil {
		slog.Info("password-reset request for unknown email (silently dropped)", "email", email)
	}

	s.render(w, "forgot_password", map[string]any{
		"Done": true,
	})
}

func (s *Server) handleResetPasswordGet(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	s.render(w, "reset_password", map[string]any{
		"Token": token,
		"Flash": r.URL.Query().Get("flash"),
	})
}

func (s *Server) handleResetPasswordPost(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if err := r.ParseForm(); err != nil {
		s.render(w, "reset_password", map[string]any{"Token": token, "Flash": "bad form"})
		return
	}
	pwd := r.FormValue("password")
	confirm := r.FormValue("password_confirm")
	if pwd == "" || len(pwd) < 8 {
		s.render(w, "reset_password", map[string]any{"Token": token, "Flash": "Password must be at least 8 characters."})
		return
	}
	if pwd != confirm {
		s.render(w, "reset_password", map[string]any{"Token": token, "Flash": "Passwords don't match."})
		return
	}
	user, err := s.store.ConsumeResetToken(r.Context(), token, pwd)
	if err != nil {
		msg := "Reset link is invalid or expired. Request a new one."
		switch {
		case errors.Is(err, store.ErrResetTokenExpired):
			msg = "This reset link has expired. Request a new one."
		case errors.Is(err, store.ErrResetTokenUsed):
			msg = "This reset link has already been used."
		}
		s.render(w, "reset_password", map[string]any{"Token": token, "Flash": msg})
		return
	}
	ip, ua := audit.FromRequest(r)
	// Consuming a reset token proves email control — opportunistically mark verified.
	_, _ = s.store.MarkEmailVerified(r.Context(), user.ID)
	s.audit.Log(r.Context(), audit.Event{
		ActorUserID: &user.ID, ActorEmail: user.Email,
		Event:     audit.EventPasswordResetConsume,
		IPAddress: ip, UserAgent: ua,
	})
	http.Redirect(w, r, "/admin/login?flash=Password+updated.+Sign+in+below.", http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Accept invite
// ---------------------------------------------------------------------------

func (s *Server) handleAcceptInviteGet(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	inv, err := s.store.VerifyInvite(r.Context(), token)
	if err != nil {
		s.render(w, "accept_invite", map[string]any{
			"Token": token,
			"Flash": inviteErrMsg(err),
			"Bad":   true,
		})
		return
	}
	tenant, _ := s.store.GetTenant(r.Context(), inv.TenantID)
	existing, _ := s.store.GetUserByEmail(r.Context(), inv.Email)
	s.render(w, "accept_invite", map[string]any{
		"Token":      token,
		"Invite":     inv,
		"Tenant":     tenant,
		"IsExisting": existing != nil,
	})
}

func (s *Server) handleAcceptInvitePost(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	displayName := strings.TrimSpace(r.FormValue("display_name"))
	password := r.FormValue("password")

	user, err := s.store.AcceptInvite(r.Context(), token, displayName, password)
	if err != nil {
		s.render(w, "accept_invite", map[string]any{
			"Token": token,
			"Flash": inviteErrMsg(err),
			"Bad": errors.Is(err, store.ErrInvalidInviteToken) ||
				errors.Is(err, store.ErrInviteExpired) ||
				errors.Is(err, store.ErrInviteUsed) ||
				errors.Is(err, store.ErrInviteRevoked),
		})
		return
	}

	// Auto-login the new/existing user to their newly-joined tenant.
	memberships, _ := s.store.ListMembershipsForUser(r.Context(), user.ID)
	var tenantID *uuid.UUID
	scope := store.ScopeForRole(user.Role)
	if len(memberships) == 1 {
		m := memberships[0]
		tenantID = &m.TenantID
		scope = store.ScopeForRole(m.Role)
	} else if len(memberships) > 1 {
		scope = "read"
		for _, m := range memberships {
			ms := store.ScopeForRole(m.Role)
			if ms == "admin" {
				scope = "admin"
				break
			} else if ms == "write" {
				scope = "write"
			}
		}
	}
	issued, err := s.store.CreateAPIToken(r.Context(), store.CreateAPITokenInput{
		TenantID:  tenantID,
		Name:      "portal:" + user.Email + ":accept-invite",
		Scope:     scope,
		ExpiresAt: portalSessionExpiry(),
	})
	if err != nil {
		http.Redirect(w, r, "/admin/login?flash=Invite+accepted.+Please+sign+in.", http.StatusSeeOther)
		return
	}
	s.setSessionCookie(w, issued.Plaintext)
	ip, ua := audit.FromRequest(r)
	s.audit.Log(r.Context(), audit.Event{
		TenantID: tenantID, ActorUserID: &user.ID, ActorEmail: user.Email,
		ActorTokenID: &issued.ID,
		Event:        audit.EventInviteAccept,
		TargetType:   "user", TargetID: &user.ID,
		IPAddress: ip, UserAgent: ua,
	})
	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

func inviteErrMsg(err error) string {
	switch {
	case errors.Is(err, store.ErrInvalidInviteToken):
		return "Invite link is not valid."
	case errors.Is(err, store.ErrInviteExpired):
		return "This invite has expired. Ask the inviter to send a new one."
	case errors.Is(err, store.ErrInviteUsed):
		return "This invite has already been accepted. Sign in below."
	case errors.Is(err, store.ErrInviteRevoked):
		return "This invite was revoked. Ask the inviter to send a new one."
	default:
		return err.Error()
	}
}

// ---------------------------------------------------------------------------
// Tenant invites admin page (authenticated)
// ---------------------------------------------------------------------------

func (s *Server) invitesList(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	invites, _ := s.store.ListInvitesForTenant(r.Context(), tid)
	s.renderLayout(w, r, tenant.Name+" · Invites", "invites", map[string]any{
		"Tenant":    tenant,
		"Invites":   invites,
		"NewLink":   r.URL.Query().Get("new"),
		"NavActive": "invites",
	})
}

func (s *Server) invitesCreate(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	email := strings.TrimSpace(r.FormValue("email"))
	role := r.FormValue("role")
	if role != "tenant_admin" {
		role = "user"
	}
	if email == "" {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/invites", errors.New("email required"))
		return
	}
	issued, err := s.store.CreateInvite(r.Context(), store.CreateInviteInput{
		TenantID: tid, Email: email, Role: role,
	})
	if err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/invites", err)
		return
	}
	ip, ua := audit.FromRequest(r)
	tok := tokenFromCtx(r.Context())
	var actorTok *uuid.UUID
	if tok != nil {
		actorTok = &tok.ID
	}
	s.audit.Log(r.Context(), audit.Event{
		TenantID: &tid, ActorTokenID: actorTok,
		Event:      audit.EventInviteCreate,
		TargetType: "invite", TargetID: &issued.ID,
		IPAddress: ip, UserAgent: ua,
		Payload: map[string]any{"email": email, "role": role},
	})

	tenant, _ := s.store.GetTenant(r.Context(), tid)
	acceptURL := s.portalBaseURL + "/admin/accept-invite/" + issued.Plaintext
	if s.mailer != nil && tenant != nil {
		go func(to, tname, url string) {
			_, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.mailer.SendInvite(to, "", tname, url); err != nil {
				slog.Warn("invite email failed", "to", to, "err", err)
			}
		}(email, tenant.Name, acceptURL)
	}

	// Surface the accept URL on the invites page so admins can hand-deliver
	// it if SMTP isn't configured.
	http.Redirect(w, r,
		"/admin/tenants/"+tid.String()+"/invites?new="+acceptURL,
		http.StatusSeeOther)
}

func (s *Server) invitesRevoke(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "inviteID"))
	if err != nil {
		http.Error(w, "bad invite id", 400)
		return
	}
	if err := s.store.RevokeInvite(r.Context(), id); err != nil {
		s.errPage(w, r, err)
		return
	}
	ip, ua := audit.FromRequest(r)
	tok := tokenFromCtx(r.Context())
	var actorTok *uuid.UUID
	if tok != nil {
		actorTok = &tok.ID
	}
	s.audit.Log(r.Context(), audit.Event{
		ActorTokenID: actorTok,
		Event:        audit.EventInviteRevoke,
		TargetType:   "invite", TargetID: &id,
		IPAddress: ip, UserAgent: ua,
	})
	// Redirect back to wherever (Referer) since invites don't expose tenant.
	back := r.Header.Get("Referer")
	if back == "" {
		back = "/admin/"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}
