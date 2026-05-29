package portal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/crewjam/saml"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/audit"
	"github.com/tendpos/sip-platform/control-plane/internal/sso"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

const (
	cookieSAMLState = "sip_saml_state"
	samlStateTTL    = 10 * time.Minute
)

// ---------------------------------------------------------------------------
// Relay-state cookie (mirrors OIDC's sip_sso_state).
// ---------------------------------------------------------------------------

type samlState struct {
	TenantID  uuid.UUID `json:"t"`
	RequestID string    `json:"r"`  // AuthnRequest ID for replay protection
	ReturnTo  string    `json:"u"`
	Email     string    `json:"e,omitempty"`
	ExpiresAt int64     `json:"x"`
}

func (s *Server) setSAMLState(w http.ResponseWriter, st samlState) {
	st.ExpiresAt = time.Now().Add(samlStateTTL).Unix()
	b, _ := json.Marshal(st)
	http.SetCookie(w, &http.Cookie{
		Name: cookieSAMLState, Value: base64.RawURLEncoding.EncodeToString(b),
		Path: "/admin", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.Secure,
		MaxAge: int(samlStateTTL.Seconds()),
	})
}

func (s *Server) readSAMLState(r *http.Request) (*samlState, error) {
	c, err := r.Cookie(cookieSAMLState)
	if err != nil {
		return nil, err
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return nil, err
	}
	var st samlState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, err
	}
	if time.Now().Unix() > st.ExpiresAt {
		return nil, errors.New("saml state expired")
	}
	return &st, nil
}

func (s *Server) clearSAMLState(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: cookieSAMLState, Value: "", Path: "/admin", MaxAge: -1})
}

// ---------------------------------------------------------------------------
// SP setup — shared per request.
// ---------------------------------------------------------------------------

func (s *Server) buildSAMLSP(cfg *store.TenantSAMLConfig) (*saml.ServiceProvider, error) {
	if s.samlKey == nil {
		return nil, errors.New("SAML is disabled in this deployment (SAML_SP_CERT_PEM + SAML_SP_KEY_PEM not set)")
	}
	entityID := cfg.EntityIDOverride
	if entityID == "" {
		entityID = s.portalBaseURL + "/admin/sso/saml/metadata"
	}
	acsURL, err := url.Parse(s.portalBaseURL + "/admin/sso/saml/callback")
	if err != nil {
		return nil, err
	}
	mdURL, err := url.Parse(s.portalBaseURL + "/admin/sso/saml/metadata")
	if err != nil {
		return nil, err
	}
	return sso.NewSAMLSP(sso.SAMLSPInput{
		Keypair:        s.samlKey,
		IDPMetadataXML: []byte(cfg.IDPMetadataXML),
		EntityID:       entityID,
		AcsURL:         acsURL,
		MetadataURL:    mdURL,
	})
}

// ---------------------------------------------------------------------------
// /admin/sso/saml/metadata — public SP metadata (XML) for IdP admins.
// ---------------------------------------------------------------------------

func (s *Server) samlMetadata(w http.ResponseWriter, r *http.Request) {
	if s.samlKey == nil {
		http.Error(w, "SAML disabled (SAML_SP_CERT_PEM/SAML_SP_KEY_PEM unset)", http.StatusServiceUnavailable)
		return
	}
	// Use any tenant config? We don't need a tenant for metadata — the SP
	// keypair + URLs are platform-wide. Build a config-less SP.
	acsURL, _ := url.Parse(s.portalBaseURL + "/admin/sso/saml/callback")
	mdURL, _ := url.Parse(s.portalBaseURL + "/admin/sso/saml/metadata")
	sp := &saml.ServiceProvider{
		EntityID:    s.portalBaseURL + "/admin/sso/saml/metadata",
		Key:         s.samlKey.Key,
		Certificate: s.samlKey.Cert,
		AcsURL:      *acsURL,
		MetadataURL: *mdURL,
	}
	md := sp.Metadata()
	buf, err := xmlMarshal(md)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/samlmetadata+xml")
	_, _ = w.Write(buf)
}

// xmlMarshal is split out so we can swap it for indented output in tests.
func xmlMarshal(v any) ([]byte, error) {
	return crewjamMarshal(v)
}

// ---------------------------------------------------------------------------
// Login entry points
// ---------------------------------------------------------------------------

// handleSAMLLoginByEmail is wired alongside the OIDC variant. The main
// /admin/login/sso POST tries OIDC first; if no OIDC config matches the
// email domain, it falls through to this SAML lookup.
func (s *Server) handleSAMLLoginByEmail(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		http.Redirect(w, r, "/admin/login?flash=Enter+your+work+email+to+start+SSO.", http.StatusSeeOther)
		return
	}
	tenant, cfg, _ := s.store.LookupSAMLByEmailDomain(r.Context(), email)
	if tenant == nil || cfg == nil {
		http.Redirect(w, r, "/admin/login?flash=No+SSO+configured+for+that+email+domain.", http.StatusSeeOther)
		return
	}
	s.startSAMLFlow(w, r, tenant, cfg, email)
}

func (s *Server) handleSAMLLoginByTenant(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "tenantSlug")
	tenant, err := s.store.GetTenantBySlug(r.Context(), slug)
	if err != nil || tenant == nil {
		http.Redirect(w, r, "/admin/login?flash=Unknown+workspace.", http.StatusSeeOther)
		return
	}
	cfg, err := s.store.GetTenantSAMLConfig(r.Context(), tenant.ID)
	if err != nil || cfg == nil || !cfg.Enabled {
		http.Redirect(w, r, "/admin/login?flash=SAML+is+not+enabled+for+that+workspace.", http.StatusSeeOther)
		return
	}
	s.startSAMLFlow(w, r, tenant, cfg, "")
}

func (s *Server) startSAMLFlow(w http.ResponseWriter, r *http.Request, tenant *store.Tenant, cfg *store.TenantSAMLConfig, emailHint string) {
	sp, err := s.buildSAMLSP(cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	// MakeAuthenticationRequest mints a fresh ID we'll need to validate the
	// ACS response against. We extract it from the resulting struct.
	req, err := sp.MakeAuthenticationRequest(
		sp.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding,
		saml.HTTPPostBinding,
	)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	relayState := uuid.NewString()
	redirectURL, err := req.Redirect(relayState, sp)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	s.setSAMLState(w, samlState{
		TenantID: tenant.ID, RequestID: req.ID,
		ReturnTo: "/admin/", Email: emailHint,
	})
	http.Redirect(w, r, redirectURL.String(), http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Assertion Consumer Service (ACS) — HTTP-POST binding
// ---------------------------------------------------------------------------

func (s *Server) handleSAMLCallback(w http.ResponseWriter, r *http.Request) {
	st, err := s.readSAMLState(r)
	if err != nil {
		http.Redirect(w, r, "/admin/login?flash=SAML+session+expired+—+sign+in+again.", http.StatusSeeOther)
		return
	}
	s.clearSAMLState(w)

	cfg, err := s.store.GetTenantSAMLConfig(r.Context(), st.TenantID)
	if err != nil || cfg == nil || !cfg.Enabled {
		http.Redirect(w, r, "/admin/login?flash=SAML+disabled+for+that+workspace.", http.StatusSeeOther)
		return
	}
	sp, err := s.buildSAMLSP(cfg)
	if err != nil {
		s.errPage(w, r, err)
		return
	}

	assertion, err := sso.ParseACSResponse(sp, r, []string{st.RequestID})
	if err != nil {
		ip, ua := audit.FromRequest(r)
		s.audit.Log(r.Context(), audit.Event{
			TenantID: &st.TenantID, Event: "sso.login.failure",
			IPAddress: ip, UserAgent: ua,
			Payload: map[string]any{"protocol": "saml", "error": err.Error()},
		})
		http.Redirect(w, r, "/admin/login?flash=SAML+assertion+invalid.", http.StatusSeeOther)
		return
	}

	email := sso.ExtractAttribute(assertion, cfg.AttrEmail, true)
	name := sso.ExtractAttribute(assertion, cfg.AttrName, false)
	if email == "" {
		http.Redirect(w, r, "/admin/login?flash=SAML+assertion+had+no+email+attribute.", http.StatusSeeOther)
		return
	}
	issuer := assertion.Issuer.Value
	subject := ""
	if assertion.Subject != nil && assertion.Subject.NameID != nil {
		subject = assertion.Subject.NameID.Value
	}
	rawClaims := map[string]any{
		"email":   email,
		"name":    name,
		"issuer":  issuer,
		"subject": subject,
	}

	res, err := s.store.ProvisionFromSSO(r.Context(), store.JITProvisionInput{
		TenantID:     st.TenantID,
		ProviderKind: "saml",
		Issuer:       issuer,
		Subject:      subject,
		Email:        email,
		DisplayName:  name,
		RawClaims:    rawClaims,
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
			Payload: map[string]any{"protocol": "saml", "issuer": issuer},
		})
	case res.WasLinked:
		s.audit.Log(r.Context(), audit.Event{
			TenantID: &st.TenantID, ActorUserID: &res.User.ID, ActorEmail: res.User.Email,
			Event: "sso.jit.user_linked", IPAddress: ip, UserAgent: ua,
			Payload: map[string]any{"protocol": "saml", "issuer": issuer},
		})
	}

	memberships, _ := s.store.ListMembershipsForUser(r.Context(), res.User.ID)
	scope := store.ScopeForRole(res.User.Role)
	for _, m := range memberships {
		if m.TenantID == st.TenantID {
			scope = store.ScopeForRole(m.Role)
			break
		}
	}
	issued, err := s.store.CreateAPIToken(r.Context(), store.CreateAPITokenInput{
		TenantID: &st.TenantID,
		Name:     "portal:" + res.User.Email + ":saml:" + time.Now().UTC().Format("20060102T150405"),
		Scope:    scope,
	})
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	s.setSessionCookie(w, issued.Plaintext)
	s.audit.Log(r.Context(), audit.Event{
		TenantID: &st.TenantID, ActorUserID: &res.User.ID, ActorEmail: res.User.Email,
		ActorTokenID: &issued.ID,
		Event:        "sso.login.success",
		IPAddress:    ip, UserAgent: ua,
		Payload: map[string]any{"protocol": "saml", "issuer": issuer, "scope": scope},
	})
	target := st.ReturnTo
	if target == "" {
		target = "/admin/"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Tenant SAML admin
// ---------------------------------------------------------------------------

func (s *Server) tenantSAMLGet(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	cfg, _ := s.store.GetTenantSAMLConfig(r.Context(), tid)
	hasOIDC, _ := s.store.HasSSOConfigured(r.Context(), tid, "saml")
	s.renderLayout(w, r, tenant.Name+" · SAML", "tenant_saml", map[string]any{
		"Tenant":     tenant,
		"Config":     cfg,
		"HasOIDC":    hasOIDC,
		"KeyReady":   s.samlKey != nil,
		"PortalBase": s.portalBaseURL,
	})
}

func (s *Server) tenantSAMLSave(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	if s.samlKey == nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/saml",
			errors.New("SAML disabled (SAML_SP_CERT_PEM/SAML_SP_KEY_PEM unset)"))
		return
	}
	if blocked, _ := s.store.HasSSOConfigured(r.Context(), tid, "saml"); blocked {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/saml", store.ErrSSOAlreadyConfigured)
		return
	}
	idpXML := strings.TrimSpace(r.FormValue("idp_metadata_xml"))
	idpURL := strings.TrimSpace(r.FormValue("idp_metadata_url"))
	if idpXML == "" && idpURL != "" {
		// Best-effort fetch on save.
		fetched, err := fetchIdPMetadata(r.Context(), idpURL)
		if err != nil {
			s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/saml",
				errors.New("failed to fetch metadata URL: "+err.Error()))
			return
		}
		idpXML = string(fetched)
	}
	if idpXML == "" {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/saml",
			errors.New("paste IdP metadata XML or provide a metadata URL"))
		return
	}
	_, err := s.store.SaveTenantSAMLConfig(r.Context(), store.SaveTenantSAMLConfigInput{
		TenantID:         tid,
		Label:            strings.TrimSpace(r.FormValue("label")),
		IDPMetadataXML:   idpXML,
		IDPMetadataURL:   idpURL,
		EntityIDOverride: strings.TrimSpace(r.FormValue("entity_id_override")),
		AttrEmail:        strings.TrimSpace(r.FormValue("attr_email")),
		AttrName:         strings.TrimSpace(r.FormValue("attr_name")),
		Enabled:          r.FormValue("enabled") == "true",
	})
	if err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/saml", err)
		return
	}
	ip, ua := audit.FromRequest(r)
	var actorTok *uuid.UUID
	if tok := tokenFromCtx(r.Context()); tok != nil {
		actorTok = &tok.ID
	}
	s.audit.Log(r.Context(), audit.Event{
		TenantID: &tid, ActorTokenID: actorTok,
		Event: "sso.config.update", TargetType: "tenant", TargetID: &tid,
		IPAddress: ip, UserAgent: ua,
		Payload: map[string]any{"protocol": "saml"},
	})
	http.Redirect(w, r, "/admin/tenants/"+tid.String()+"/saml?flash=saved", http.StatusSeeOther)
}

func (s *Server) tenantSAMLDisable(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := s.store.DisableTenantSAMLConfig(r.Context(), tid); err != nil {
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
		Event: "sso.config.disable", TargetType: "tenant", TargetID: &tid,
		IPAddress: ip, UserAgent: ua,
		Payload: map[string]any{"protocol": "saml"},
	})
	http.Redirect(w, r, "/admin/tenants/"+tid.String()+"/saml?flash=disabled", http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func fetchIdPMetadata(ctx context.Context, u string) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, errors.New("metadata fetch returned " + resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB cap
}
