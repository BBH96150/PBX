package portal

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/tendpos/sip-platform/control-plane/internal/audit"
	"github.com/tendpos/sip-platform/control-plane/internal/crypto"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

const (
	cookie2FAPending = "sip_2fa_pending"
	cookie2FATrust   = "sip_2fa_trust"
	pending2FATTL    = 5 * time.Minute
	trustDeviceTTL   = 30 * 24 * time.Hour
	totpIssuer       = "SIP Platform"
)

// ---------------------------------------------------------------------------
// Pending-2FA cookie. Holds the user+scope+tenant decided at password-verify
// time, while the user completes the challenge. NOT a session token — it
// can't authenticate any other request.
// ---------------------------------------------------------------------------

type pending2FAState struct {
	UserID    uuid.UUID  `json:"u"`
	TenantID  *uuid.UUID `json:"t,omitempty"`
	Scope     string     `json:"s"`
	ExpiresAt int64      `json:"e"`
}

func (s *Server) setPending2FA(w http.ResponseWriter, st pending2FAState) {
	st.ExpiresAt = time.Now().Add(pending2FATTL).Unix()
	b, _ := json.Marshal(st)
	v := base64.RawURLEncoding.EncodeToString(b)
	http.SetCookie(w, &http.Cookie{
		Name: cookie2FAPending, Value: v, Path: "/admin",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.Secure,
		MaxAge: int(pending2FATTL.Seconds()),
	})
}

func (s *Server) readPending2FA(r *http.Request) (*pending2FAState, error) {
	c, err := r.Cookie(cookie2FAPending)
	if err != nil {
		return nil, err
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return nil, err
	}
	var st pending2FAState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, err
	}
	if time.Now().Unix() > st.ExpiresAt {
		return nil, errors.New("pending 2fa expired")
	}
	return &st, nil
}

func (s *Server) clearPending2FA(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: cookie2FAPending, Value: "", Path: "/admin", MaxAge: -1,
	})
}

// ---------------------------------------------------------------------------
// Login interstitial: called from handleLoginPost's success branch after
// password verification. Returns true if the caller should stop (we either
// redirected to challenge or mounted a session via trusted-device skip).
// ---------------------------------------------------------------------------

func (s *Server) gateLoginWith2FA(
	w http.ResponseWriter, r *http.Request,
	user *store.User, tenantID *uuid.UUID, scope string,
) (handled bool) {
	methods, _ := s.store.ListConfirmedTwoFAMethods(r.Context(), user.ID)
	if len(methods) == 0 {
		return false
	}

	ip, ua := audit.FromRequest(r)

	// Trusted-device skip?
	if c, err := r.Cookie(cookie2FATrust); err == nil && c.Value != "" {
		if dev, err := s.store.VerifyTrustedDevice(r.Context(), user.ID, c.Value); err == nil {
			_ = s.store.MarkTrustedDeviceSeen(r.Context(), dev.ID)
			s.mintSessionAndAudit(w, r, user, tenantID, scope, audit.Event{
				TenantID: tenantID, ActorUserID: &user.ID, ActorEmail: user.Email,
				Event:     "2fa.trusted_device.skip",
				IPAddress: ip, UserAgent: ua,
				Payload: map[string]any{"device_id": dev.ID.String(), "label": dev.Label},
			})
			return true
		}
		// Bad/expired cookie — clear it and fall through to challenge.
		http.SetCookie(w, &http.Cookie{Name: cookie2FATrust, Value: "", Path: "/admin", MaxAge: -1})
	}

	s.setPending2FA(w, pending2FAState{
		UserID: user.ID, TenantID: tenantID, Scope: scope,
	})
	http.Redirect(w, r, "/admin/2fa-challenge", http.StatusSeeOther)
	return true
}

// mintSessionAndAudit is the shared "real session" path. Used by the
// trusted-device skip and by the post-challenge success.
func (s *Server) mintSessionAndAudit(
	w http.ResponseWriter, r *http.Request,
	user *store.User, tenantID *uuid.UUID, scope string,
	auditEvt audit.Event,
) {
	issued, err := s.store.CreateAPIToken(r.Context(), store.CreateAPITokenInput{
		TenantID:  tenantID,
		Name:      "portal:" + user.Email + ":" + time.Now().UTC().Format("20060102T150405"),
		Scope:     scope,
		ExpiresAt: portalSessionExpiry(),
	})
	if err != nil {
		s.render(w, "login", map[string]any{"Flash": "Internal error creating session."})
		return
	}
	s.setSessionCookie(w, issued.Plaintext)
	auditEvt.ActorTokenID = &issued.ID
	s.audit.Log(r.Context(), auditEvt)
	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Challenge page.
// ---------------------------------------------------------------------------

func (s *Server) twoFAChallengeGet(w http.ResponseWriter, r *http.Request) {
	st, err := s.readPending2FA(r)
	if err != nil {
		http.Redirect(w, r, "/admin/login?flash=Sign+in+again.", http.StatusSeeOther)
		return
	}
	_ = st
	s.render(w, "twofa_challenge", map[string]any{
		"Flash": r.URL.Query().Get("flash"),
	})
}

func (s *Server) twoFAChallengePost(w http.ResponseWriter, r *http.Request) {
	st, err := s.readPending2FA(r)
	if err != nil {
		http.Redirect(w, r, "/admin/login?flash=Sign+in+again.", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	code := strings.TrimSpace(r.FormValue("code"))
	recovery := strings.TrimSpace(r.FormValue("recovery"))
	rememberDevice := r.FormValue("remember") == "true"

	user, err := s.store.GetUserByID(r.Context(), st.UserID)
	if err != nil {
		http.Redirect(w, r, "/admin/login?flash=Sign+in+again.", http.StatusSeeOther)
		return
	}
	ip, ua := audit.FromRequest(r)

	// Try recovery code first if supplied.
	if recovery != "" {
		if err := s.store.ConsumeRecoveryCode(r.Context(), user.ID, recovery); err != nil {
			s.audit.Log(r.Context(), audit.Event{
				TenantID: st.TenantID, ActorUserID: &user.ID, ActorEmail: user.Email,
				Event: "2fa.challenge.failure", IPAddress: ip, UserAgent: ua,
				Payload: map[string]any{"method": "recovery"},
			})
			s.render(w, "twofa_challenge", map[string]any{"Flash": "Invalid recovery code."})
			return
		}
		s.audit.Log(r.Context(), audit.Event{
			TenantID: st.TenantID, ActorUserID: &user.ID, ActorEmail: user.Email,
			Event: "2fa.recovery_used", IPAddress: ip, UserAgent: ua,
		})
		s.finishChallengeAndMint(w, r, user, st, rememberDevice, "recovery")
		return
	}

	// Otherwise validate against every confirmed TOTP method.
	if code == "" {
		s.render(w, "twofa_challenge", map[string]any{"Flash": "Enter the 6-digit code from your authenticator app."})
		return
	}
	methods, _ := s.store.ListConfirmedTwoFAMethods(r.Context(), user.ID)
	matched := s.matchTOTP(methods, code)
	if matched == nil {
		s.audit.Log(r.Context(), audit.Event{
			TenantID: st.TenantID, ActorUserID: &user.ID, ActorEmail: user.Email,
			Event: "2fa.challenge.failure", IPAddress: ip, UserAgent: ua,
			Payload: map[string]any{"method": "totp"},
		})
		s.render(w, "twofa_challenge", map[string]any{"Flash": "Invalid code. Check your authenticator app and try again."})
		return
	}
	_ = s.store.MarkTwoFAMethodUsed(r.Context(), matched.ID)
	s.audit.Log(r.Context(), audit.Event{
		TenantID: st.TenantID, ActorUserID: &user.ID, ActorEmail: user.Email,
		Event: "2fa.challenge.success", IPAddress: ip, UserAgent: ua,
	})
	s.finishChallengeAndMint(w, r, user, st, rememberDevice, "totp")
}

func (s *Server) matchTOTP(methods []store.TwoFAMethod, code string) *store.TwoFAMethod {
	if s.sealer == nil {
		return nil
	}
	for i := range methods {
		m := methods[i]
		if m.Kind != "totp" {
			continue
		}
		secretB, err := s.sealer.Open(m.SecretCiphertext, m.SecretNonce)
		if err != nil {
			continue
		}
		if totp.Validate(code, string(secretB)) {
			return &m
		}
	}
	return nil
}

func (s *Server) finishChallengeAndMint(
	w http.ResponseWriter, r *http.Request,
	user *store.User, st *pending2FAState,
	rememberDevice bool, method string,
) {
	s.clearPending2FA(w)

	if rememberDevice {
		ip, ua := audit.FromRequest(r)
		label := deviceLabelFromUA(ua)
		dev, err := s.store.CreateTrustedDevice(r.Context(), user.ID, label, ip)
		if err == nil {
			http.SetCookie(w, &http.Cookie{
				Name: cookie2FATrust, Value: dev.Plaintext, Path: "/admin",
				HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.Secure,
				MaxAge: int(trustDeviceTTL.Seconds()),
			})
			s.audit.Log(r.Context(), audit.Event{
				TenantID: st.TenantID, ActorUserID: &user.ID, ActorEmail: user.Email,
				Event: "2fa.trusted_device.add", IPAddress: ip, UserAgent: ua,
				Payload: map[string]any{"device_id": dev.ID.String(), "label": label},
			})
		}
	}

	ip, ua := audit.FromRequest(r)
	s.mintSessionAndAudit(w, r, user, st.TenantID, st.Scope, audit.Event{
		TenantID: st.TenantID, ActorUserID: &user.ID, ActorEmail: user.Email,
		Event: audit.EventLoginSuccess, IPAddress: ip, UserAgent: ua,
		Payload: map[string]any{"with_2fa": method},
	})
}

// ---------------------------------------------------------------------------
// Enrollment — /admin/security/2fa
// ---------------------------------------------------------------------------

func (s *Server) twoFAStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}
	methods, _ := s.store.ListConfirmedTwoFAMethods(r.Context(), user.ID)
	remainingCodes, _ := s.store.CountUnusedRecoveryCodes(r.Context(), user.ID)
	devices, _ := s.store.ListTrustedDevices(r.Context(), user.ID)
	s.renderLayout(w, r, "Two-factor authentication", "twofa_status", map[string]any{
		"User":              user,
		"Methods":           methods,
		"RecoveryRemaining": remainingCodes,
		"Devices":           devices,
		"SealerReady":       s.sealer != nil,
	})
}

// twoFASetupPost generates a fresh secret, stores it (unconfirmed), and
// renders the QR + provisioning URI for the user to scan.
func (s *Server) twoFASetupPost(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}
	if s.sealer == nil {
		s.flashErr(w, r, "/admin/security/2fa", errors.New("2FA is disabled in this deployment (TOTP_ENCRYPTION_KEY not set)"))
		return
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: user.Email,
	})
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	ct, nonce, err := s.sealer.Seal([]byte(key.Secret()))
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	_ = r.ParseForm()
	label := strings.TrimSpace(r.FormValue("label"))
	if label == "" {
		label = "authenticator"
	}
	if _, err := s.store.CreatePendingTwoFAMethod(r.Context(), store.CreateTwoFAMethodInput{
		UserID: user.ID, Kind: "totp", SecretCiphertext: ct, SecretNonce: nonce, Label: label,
	}); err != nil {
		s.errPage(w, r, err)
		return
	}
	png, err := qrcode.Encode(key.URL(), qrcode.Medium, 256)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	s.renderLayout(w, r, "Set up authenticator", "twofa_setup", map[string]any{
		"Label":      label,
		"Secret":     key.Secret(), // shown for manual entry
		"ProvURI":    key.URL(),
		"QRDataURL":  template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png)),
	})
}

// twoFAConfirmPost finalizes enrollment: validates the first 6-digit code,
// stamps confirmed_at, generates 10 recovery codes, shows them once.
func (s *Server) twoFAConfirmPost(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	label := strings.TrimSpace(r.FormValue("label"))
	if label == "" {
		label = "authenticator"
	}
	code := strings.TrimSpace(r.FormValue("code"))
	// Find the matching pending method by label.
	methods, err := s.store.ListConfirmedTwoFAMethods(r.Context(), user.ID)
	_ = methods // we only care about pending below
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	pending, err := s.findPendingTOTP(r.Context(), user.ID, label)
	if err != nil {
		s.flashErr(w, r, "/admin/security/2fa", errors.New("no pending setup — start over"))
		return
	}
	if s.sealer == nil {
		s.flashErr(w, r, "/admin/security/2fa", crypto.ErrNoKey)
		return
	}
	secretB, err := s.sealer.Open(pending.SecretCiphertext, pending.SecretNonce)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	if !totp.Validate(code, string(secretB)) {
		s.flashErr(w, r, "/admin/security/2fa", errors.New("code didn't match — try again from the setup page"))
		return
	}
	if err := s.store.ConfirmTwoFAMethod(r.Context(), pending.ID); err != nil {
		s.errPage(w, r, err)
		return
	}
	issued, err := s.store.GenerateRecoveryCodes(r.Context(), user.ID)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	ip, ua := audit.FromRequest(r)
	s.audit.Log(r.Context(), audit.Event{
		ActorUserID: &user.ID, ActorEmail: user.Email,
		Event: "2fa.enroll", IPAddress: ip, UserAgent: ua,
		Payload: map[string]any{"kind": "totp", "label": label},
	})
	s.renderLayout(w, r, "Save your recovery codes", "twofa_recovery_codes", map[string]any{
		"Codes": issued.Plaintext,
	})
}

func (s *Server) findPendingTOTP(ctx context.Context, userID uuid.UUID, label string) (*store.TwoFAMethod, error) {
	const q = `
		SELECT id, user_id, kind, secret_ciphertext, secret_nonce,
		       label, confirmed_at, last_used_at, created_at
		  FROM user_2fa_methods
		 WHERE user_id = $1 AND kind = 'totp' AND label = $2
		 ORDER BY created_at DESC LIMIT 1`
	var m store.TwoFAMethod
	err := s.store.DB.QueryRow(ctx, q, userID, label).Scan(
		&m.ID, &m.UserID, &m.Kind, &m.SecretCiphertext, &m.SecretNonce,
		&m.Label, &m.ConfirmedAt, &m.LastUsedAt, &m.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// twoFADisablePost — user clears their own 2FA. Requires a current code or
// recovery code to confirm.
func (s *Server) twoFADisablePost(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	code := strings.TrimSpace(r.FormValue("code"))
	recovery := strings.TrimSpace(r.FormValue("recovery"))

	methods, _ := s.store.ListConfirmedTwoFAMethods(r.Context(), user.ID)
	if len(methods) == 0 {
		http.Redirect(w, r, "/admin/security/2fa?flash=already+disabled", http.StatusSeeOther)
		return
	}
	authorized := false
	if recovery != "" {
		if err := s.store.ConsumeRecoveryCode(r.Context(), user.ID, recovery); err == nil {
			authorized = true
		}
	}
	if !authorized && code != "" && s.matchTOTP(methods, code) != nil {
		authorized = true
	}
	if !authorized {
		s.flashErr(w, r, "/admin/security/2fa", errors.New("supply a current 6-digit code or a recovery code to disable"))
		return
	}
	if err := s.store.ResetTwoFAForUser(r.Context(), user.ID); err != nil {
		s.errPage(w, r, err)
		return
	}
	ip, ua := audit.FromRequest(r)
	s.audit.Log(r.Context(), audit.Event{
		ActorUserID: &user.ID, ActorEmail: user.Email,
		Event: "2fa.disable", IPAddress: ip, UserAgent: ua,
	})
	http.Redirect(w, r, "/admin/security/2fa?flash=2FA+disabled", http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Trusted devices
// ---------------------------------------------------------------------------

func (s *Server) trustedDeviceRevoke(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "deviceID"))
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	if err := s.store.RevokeTrustedDevice(r.Context(), id, user.ID); err != nil {
		s.flashErr(w, r, "/admin/security/2fa", err)
		return
	}
	ip, ua := audit.FromRequest(r)
	s.audit.Log(r.Context(), audit.Event{
		ActorUserID: &user.ID, ActorEmail: user.Email,
		Event: "2fa.trusted_device.revoke", IPAddress: ip, UserAgent: ua,
		TargetType: "trusted_device", TargetID: &id,
	})
	http.Redirect(w, r, "/admin/security/2fa?flash=device+revoked", http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Admin reset — tenant_admin can reset 2FA for any member of their tenant;
// super-admin can reset anyone.
// ---------------------------------------------------------------------------

func (s *Server) admin2FAReset(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}
	targetID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	target, err := s.store.GetUserByID(r.Context(), targetID)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	authorized := false
	switch actor.Role {
	case "super_admin":
		authorized = true
	case "tenant_admin":
		actorMemberships, _ := s.store.ListMembershipsForUser(r.Context(), actor.ID)
		targetMemberships, _ := s.store.ListMembershipsForUser(r.Context(), target.ID)
		for _, am := range actorMemberships {
			if am.Role != "tenant_admin" {
				continue
			}
			for _, tm := range targetMemberships {
				if am.TenantID == tm.TenantID {
					authorized = true
					break
				}
			}
			if authorized {
				break
			}
		}
	}
	if !authorized {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := s.store.ResetTwoFAForUser(r.Context(), target.ID); err != nil {
		s.errPage(w, r, err)
		return
	}
	ip, ua := audit.FromRequest(r)
	s.audit.Log(r.Context(), audit.Event{
		ActorUserID: &actor.ID, ActorEmail: actor.Email,
		Event: "2fa.reset_by_admin", IPAddress: ip, UserAgent: ua,
		TargetType: "user", TargetID: &target.ID,
	})
	back := r.Header.Get("Referer")
	if back == "" {
		back = "/admin/"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (s *Server) requireSessionUser(w http.ResponseWriter, r *http.Request) (*store.User, bool) {
	tok := tokenFromCtx(r.Context())
	if tok == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return nil, false
	}
	user, err := s.userFromSessionToken(r.Context(), tok)
	if err != nil || user == nil {
		http.Error(w, "this page requires a real user session (not a raw API token)", http.StatusForbidden)
		return nil, false
	}
	return user, true
}

// deviceLabelFromUA is a very dumb UA-string summary for the trusted-device
// list. Enough to be recognizable; we don't try to fingerprint.
func deviceLabelFromUA(ua string) string {
	low := strings.ToLower(ua)
	browser := "Browser"
	switch {
	case strings.Contains(low, "firefox/"):
		browser = "Firefox"
	case strings.Contains(low, "edg/"):
		browser = "Edge"
	case strings.Contains(low, "chrome/"):
		browser = "Chrome"
	case strings.Contains(low, "safari/"):
		browser = "Safari"
	case strings.Contains(low, "curl"):
		browser = "curl"
	}
	plat := "device"
	switch {
	case strings.Contains(low, "mac os x"), strings.Contains(low, "macintosh"):
		plat = "Mac"
	case strings.Contains(low, "windows"):
		plat = "Windows"
	case strings.Contains(low, "iphone"):
		plat = "iPhone"
	case strings.Contains(low, "android"):
		plat = "Android"
	case strings.Contains(low, "linux"):
		plat = "Linux"
	}
	return browser + " on " + plat
}

// Compile-time check: keep imports referenced.
var _ = bytes.NewBuffer
