package portal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/audit"
	"github.com/tendpos/sip-platform/control-plane/internal/sso"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

const (
	cookieSSOState = "sip_sso_state"
	ssoStateTTL    = 10 * time.Minute
)

// ---------------------------------------------------------------------------
// State cookie — holds everything the callback needs.
// ---------------------------------------------------------------------------

type ssoState struct {
	TenantID      uuid.UUID `json:"t"`
	State         string    `json:"s"`
	Nonce         string    `json:"n"`
	PKCEVerifier  string    `json:"p"`
	ReturnTo      string    `json:"r"`
	Email         string    `json:"e,omitempty"` // hint from email-driven entry
	ExpiresAt     int64     `json:"x"`
}

func (s *Server) setSSOState(w http.ResponseWriter, st ssoState) {
	st.ExpiresAt = time.Now().Add(ssoStateTTL).Unix()
	b, _ := json.Marshal(st)
	http.SetCookie(w, &http.Cookie{
		Name: cookieSSOState, Value: base64.RawURLEncoding.EncodeToString(b),
		Path: "/admin", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.Secure,
		MaxAge: int(ssoStateTTL.Seconds()),
	})
}

func (s *Server) readSSOState(r *http.Request) (*ssoState, error) {
	c, err := r.Cookie(cookieSSOState)
	if err != nil {
		return nil, err
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return nil, err
	}
	var st ssoState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, err
	}
	if time.Now().Unix() > st.ExpiresAt {
		return nil, errors.New("sso state expired")
	}
	return &st, nil
}

func (s *Server) clearSSOState(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: cookieSSOState, Value: "", Path: "/admin", MaxAge: -1})
}

// ---------------------------------------------------------------------------
// Login entry points
// ---------------------------------------------------------------------------

// handleSSOLoginByEmail accepts an email POSTed from the main login form,
// looks up the tenant by domain, and either kicks off SSO or returns to the
// login page with a flash.
func (s *Server) handleSSOLoginByEmail(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		http.Redirect(w, r, "/admin/login?flash=Enter+your+work+email+to+start+SSO.", http.StatusSeeOther)
		return
	}
	tenant, cfg, _ := s.store.LookupSSOByEmailDomain(r.Context(), email)
	if tenant != nil && cfg != nil {
		s.startSSOFlow(w, r, tenant, cfg, email)
		return
	}
	// Phase 4.10: fall through to SAML lookup. A tenant has at most one
	// SSO provider, so this never picks both.
	samlTenant, samlCfg, _ := s.store.LookupSAMLByEmailDomain(r.Context(), email)
	if samlTenant != nil && samlCfg != nil {
		s.startSAMLFlow(w, r, samlTenant, samlCfg, email)
		return
	}
	http.Redirect(w, r, "/admin/login?flash=No+SSO+is+configured+for+that+email+domain.", http.StatusSeeOther)
}

// handleSSOLoginByTenant is the bookmarkable per-tenant URL
// (/admin/sso/{tenantSlug}/login).
func (s *Server) handleSSOLoginByTenant(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "tenantSlug")
	tenant, err := s.store.GetTenantBySlug(r.Context(), slug)
	if err != nil || tenant == nil {
		http.Redirect(w, r, "/admin/login?flash=Unknown+workspace.", http.StatusSeeOther)
		return
	}
	cfg, err := s.store.GetTenantSSOConfig(r.Context(), tenant.ID)
	if err != nil || cfg == nil || !cfg.Enabled {
		http.Redirect(w, r, "/admin/login?flash=SSO+is+not+enabled+for+that+workspace.", http.StatusSeeOther)
		return
	}
	s.startSSOFlow(w, r, tenant, cfg, "")
}

func (s *Server) startSSOFlow(w http.ResponseWriter, r *http.Request, tenant *store.Tenant, cfg *store.TenantSSOConfig, emailHint string) {
	if s.sealer == nil || s.ssoMgr == nil {
		http.Error(w, "SSO is not configured in this deployment", http.StatusServiceUnavailable)
		return
	}
	secretB, err := s.sealer.Open(cfg.ClientSecretCiphertext, cfg.ClientSecretNonce)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	state := sso.NewState()
	nonce := sso.NewNonce()
	pkce := sso.NewPKCEVerifier()
	url, err := s.ssoMgr.AuthURL(r.Context(), sso.Config{
		IssuerURL:    cfg.IssuerURL,
		ClientID:     cfg.ClientID,
		ClientSecret: string(secretB),
		RedirectURL:  s.portalBaseURL + "/admin/sso/callback",
		Scopes:       splitScopes(cfg.Scopes),
	}, state, nonce, pkce)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	s.setSSOState(w, ssoState{
		TenantID: tenant.ID, State: state, Nonce: nonce, PKCEVerifier: pkce,
		Email: emailHint, ReturnTo: "/admin/",
	})
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Callback
// ---------------------------------------------------------------------------

func (s *Server) handleSSOCallback(w http.ResponseWriter, r *http.Request) {
	st, err := s.readSSOState(r)
	if err != nil {
		http.Redirect(w, r, "/admin/login?flash=SSO+session+expired+—+sign+in+again.", http.StatusSeeOther)
		return
	}
	s.clearSSOState(w)

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		desc := r.URL.Query().Get("error_description")
		ip, ua := audit.FromRequest(r)
		s.audit.Log(r.Context(), audit.Event{
			TenantID: &st.TenantID, Event: "sso.login.failure",
			IPAddress: ip, UserAgent: ua,
			Payload: map[string]any{"error": errParam, "description": desc},
		})
		http.Redirect(w, r, "/admin/login?flash=SSO+sign-in+was+cancelled+or+failed.", http.StatusSeeOther)
		return
	}

	gotState := r.URL.Query().Get("state")
	if gotState != st.State {
		http.Redirect(w, r, "/admin/login?flash=SSO+state+mismatch+(possible+CSRF).", http.StatusSeeOther)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, "/admin/login?flash=SSO+callback+missing+code.", http.StatusSeeOther)
		return
	}

	cfg, err := s.store.GetTenantSSOConfig(r.Context(), st.TenantID)
	if err != nil || cfg == nil || !cfg.Enabled {
		http.Redirect(w, r, "/admin/login?flash=SSO+is+disabled+for+that+workspace.", http.StatusSeeOther)
		return
	}
	secretB, err := s.sealer.Open(cfg.ClientSecretCiphertext, cfg.ClientSecretNonce)
	if err != nil {
		s.errPage(w, r, err)
		return
	}

	tokens, err := s.ssoMgr.Exchange(r.Context(), sso.Config{
		IssuerURL:    cfg.IssuerURL,
		ClientID:     cfg.ClientID,
		ClientSecret: string(secretB),
		RedirectURL:  s.portalBaseURL + "/admin/sso/callback",
		Scopes:       splitScopes(cfg.Scopes),
	}, code, st.PKCEVerifier, st.Nonce)
	if err != nil {
		ip, ua := audit.FromRequest(r)
		s.audit.Log(r.Context(), audit.Event{
			TenantID: &st.TenantID, Event: "sso.login.failure",
			IPAddress: ip, UserAgent: ua,
			Payload: map[string]any{"error": err.Error()},
		})
		http.Redirect(w, r, "/admin/login?flash=SSO+token+exchange+failed.", http.StatusSeeOther)
		return
	}
	if tokens.Email == "" {
		http.Redirect(w, r, "/admin/login?flash=IdP+did+not+return+an+email+claim.", http.StatusSeeOther)
		return
	}

	res, err := s.store.ProvisionFromSSO(r.Context(), store.JITProvisionInput{
		TenantID:     st.TenantID,
		ProviderKind: "oidc",
		Issuer:       tokens.Issuer,
		Subject:      tokens.Subject,
		Email:        tokens.Email,
		DisplayName:  tokens.Name,
		RawClaims:    tokens.RawClaims,
	})
	if err != nil {
		s.errPage(w, r, err)
		return
	}

	ip, ua := audit.FromRequest(r)
	switch {
	case res.WasCreated:
		s.audit.Log(r.Context(), audit.Event{
			TenantID: &st.TenantID, ActorUserID: &res.User.ID, ActorEmail: res.User.Email,
			Event: "sso.jit.user_created", IPAddress: ip, UserAgent: ua,
			Payload: map[string]any{"issuer": tokens.Issuer},
		})
	case res.WasLinked:
		s.audit.Log(r.Context(), audit.Event{
			TenantID: &st.TenantID, ActorUserID: &res.User.ID, ActorEmail: res.User.Email,
			Event: "sso.jit.user_linked", IPAddress: ip, UserAgent: ua,
			Payload: map[string]any{"issuer": tokens.Issuer},
		})
	}

	// Mint session. SSO satisfies the local 2FA gate — IdP did MFA.
	memberships, _ := s.store.ListMembershipsForUser(r.Context(), res.User.ID)
	tenantID := &st.TenantID
	scope := store.ScopeForRole(res.User.Role)
	for _, m := range memberships {
		if m.TenantID == st.TenantID {
			scope = store.ScopeForRole(m.Role)
			break
		}
	}
	issued, err := s.store.CreateAPIToken(r.Context(), store.CreateAPITokenInput{
		TenantID: tenantID,
		Name:     "portal:" + res.User.Email + ":sso:" + time.Now().UTC().Format("20060102T150405"),
		Scope:    scope,
	})
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	s.setSessionCookie(w, issued.Plaintext)
	s.audit.Log(r.Context(), audit.Event{
		TenantID: tenantID, ActorUserID: &res.User.ID, ActorEmail: res.User.Email,
		ActorTokenID: &issued.ID,
		Event:        "sso.login.success",
		IPAddress:    ip, UserAgent: ua,
		Payload: map[string]any{"issuer": tokens.Issuer, "scope": scope},
	})

	target := st.ReturnTo
	if target == "" {
		target = "/admin/"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Tenant SSO admin
// ---------------------------------------------------------------------------

func (s *Server) tenantSSOGet(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	cfg, _ := s.store.GetTenantSSOConfig(r.Context(), tid)
	domains, _ := s.store.ListTenantSSODomains(r.Context(), tid)
	requireSSO, _ := s.store.TenantRequiresSSO(r.Context(), tid)
	s.renderLayout(w, r, tenant.Name+" · SSO", "tenant_sso", map[string]any{
		"Tenant":      tenant,
		"Config":      cfg,
		"Domains":     domains,
		"RequireSSO":  requireSSO,
		"SealerReady": s.sealer != nil,
		"PortalBase":  s.portalBaseURL,
		"Flash":       r.URL.Query().Get("flash"),
		"FlashErr":    r.URL.Query().Get("err"),
	})
}

func (s *Server) tenantSSOSave(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	if s.sealer == nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/sso", errors.New("SSO needs TOTP_ENCRYPTION_KEY (shared sealer) to store the client secret"))
		return
	}
	clientSecret := strings.TrimSpace(r.FormValue("client_secret"))
	var ct, nonce []byte
	if clientSecret != "" {
		var err error
		ct, nonce, err = s.sealer.Seal([]byte(clientSecret))
		if err != nil {
			s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/sso", err)
			return
		}
	}
	_, err := s.store.SaveTenantSSOConfig(r.Context(), store.SaveTenantSSOConfigInput{
		TenantID:               tid,
		ProviderKind:           "oidc",
		Label:                  strings.TrimSpace(r.FormValue("label")),
		IssuerURL:              strings.TrimSpace(r.FormValue("issuer_url")),
		ClientID:               strings.TrimSpace(r.FormValue("client_id")),
		ClientSecretCiphertext: ct,
		ClientSecretNonce:      nonce,
		Scopes:                 strings.TrimSpace(r.FormValue("scopes")),
		Enabled:                r.FormValue("enabled") == "true",
	})
	if err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/sso", err)
		return
	}
	requireSSO := r.FormValue("require_sso") == "true"
	if _, err := s.store.DB.Exec(r.Context(),
		`UPDATE tenants SET require_sso = $2 WHERE id = $1`, tid, requireSSO); err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/sso", err)
		return
	}

	ip, ua := audit.FromRequest(r)
	var actorTok *uuid.UUID
	if tok := tokenFromCtx(r.Context()); tok != nil {
		actorTok = &tok.ID
	}
	s.audit.Log(r.Context(), audit.Event{
		TenantID: &tid, ActorTokenID: actorTok,
		Event:      "sso.config.update",
		TargetType: "tenant", TargetID: &tid,
		IPAddress: ip, UserAgent: ua,
		Payload: map[string]any{"require_sso": requireSSO, "secret_changed": clientSecret != ""},
	})
	http.Redirect(w, r, "/admin/tenants/"+tid.String()+"/sso?flash=saved", http.StatusSeeOther)
}

func (s *Server) tenantSSODisable(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := s.store.DisableTenantSSOConfig(r.Context(), tid); err != nil {
		s.errPage(w, r, err)
		return
	}
	ip, ua := audit.FromRequest(r)
	var actorTok *uuid.UUID
	if tok := tokenFromCtx(r.Context()); tok != nil {
		actorTok = &tok.ID
	}
	s.audit.Log(r.Context(), audit.Event{
		TenantID: &tid, ActorTokenID: actorTok,
		Event:      "sso.config.disable",
		TargetType: "tenant", TargetID: &tid,
		IPAddress: ip, UserAgent: ua,
	})
	http.Redirect(w, r, "/admin/tenants/"+tid.String()+"/sso?flash=disabled", http.StatusSeeOther)
}

func (s *Server) tenantSSOTest(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	cfg, err := s.store.GetTenantSSOConfig(r.Context(), tid)
	if err != nil || cfg == nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/sso", errors.New("no config saved yet"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := s.ssoMgr.Probe(ctx, cfg.IssuerURL); err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/sso", err)
		return
	}
	http.Redirect(w, r, "/admin/tenants/"+tid.String()+"/sso?flash=Discovery+OK", http.StatusSeeOther)
}

func (s *Server) tenantSSODomainAdd(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	domain := strings.TrimSpace(strings.ToLower(r.FormValue("domain")))
	if domain == "" {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/sso", errors.New("domain required"))
		return
	}
	d, err := s.store.AddTenantSSODomain(r.Context(), tid, domain)
	if err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/sso", err)
		return
	}
	ip, ua := audit.FromRequest(r)
	s.audit.Log(r.Context(), audit.Event{
		TenantID: &tid, Event: "sso.domain.add",
		TargetType: "sso_domain", TargetID: &d.ID,
		IPAddress: ip, UserAgent: ua,
		Payload: map[string]any{"domain": domain},
	})
	http.Redirect(w, r, "/admin/tenants/"+tid.String()+"/sso?flash=domain+added", http.StatusSeeOther)
}

func (s *Server) tenantSSODomainRemove(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	did, err := uuid.Parse(chi.URLParam(r, "domainID"))
	if err != nil {
		http.Error(w, "bad domain id", 400)
		return
	}
	if err := s.store.RemoveTenantSSODomain(r.Context(), did, tid); err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/sso", err)
		return
	}
	ip, ua := audit.FromRequest(r)
	s.audit.Log(r.Context(), audit.Event{
		TenantID: &tid, Event: "sso.domain.remove",
		TargetType: "sso_domain", TargetID: &did,
		IPAddress: ip, UserAgent: ua,
	})
	http.Redirect(w, r, "/admin/tenants/"+tid.String()+"/sso?flash=domain+removed", http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Per-user SSO management
// ---------------------------------------------------------------------------

func (s *Server) userSSOIdentities(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}
	ids, _ := s.store.ListSSOIdentitiesForUser(r.Context(), user.ID)
	s.renderLayout(w, r, "Linked sign-in methods", "user_sso", map[string]any{
		"User":       user,
		"Identities": ids,
	})
}

func (s *Server) userSSOUnlink(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "identityID"))
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	if err := s.store.UnlinkSSOIdentity(r.Context(), id, user.ID); err != nil {
		s.flashErr(w, r, "/admin/security/sso", err)
		return
	}
	ip, ua := audit.FromRequest(r)
	s.audit.Log(r.Context(), audit.Event{
		ActorUserID: &user.ID, ActorEmail: user.Email,
		Event: "sso.identity.unlink", IPAddress: ip, UserAgent: ua,
		TargetType: "sso_identity", TargetID: &id,
	})
	http.Redirect(w, r, "/admin/security/sso?flash=unlinked", http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func splitScopes(s string) []string {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return []string{"openid", "email", "profile"}
	}
	return parts
}
