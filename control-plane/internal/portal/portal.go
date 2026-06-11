// Package portal is the server-rendered web admin UI for the SIP platform.
//
// Auth model: user logs in with an API token; we stash it in an HttpOnly
// cookie and verify it on every request. Portal handlers then call the
// store directly (we're in-process). Tenant scoping mirrors the API
// middleware — a tenant-scoped token can only view its own tenant.
package portal

import (
	"bytes"
	"context"
	"embed"
	"encoding/csv"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/audit"
	"github.com/tendpos/sip-platform/control-plane/internal/crypto"
	"github.com/tendpos/sip-platform/control-plane/internal/sso"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

//go:embed templates/*.html
var tmplFS embed.FS

//go:embed static/app.css static/broadcast.js static/broadcast.manifest.webmanifest static/broadcast.sw.js static/broadcast-icon.svg
var staticFS embed.FS

const cookieName = "sip_admin_token"

type Server struct {
	store      *store.Store
	tmpls      *template.Template
	mailer     Mailer
	audit      *audit.Logger
	sealer     *crypto.Sealer
	ssoMgr     *sso.Manager
	samlKey    *sso.SAMLKeypair
	gwSyncer   GatewaySyncer
	live       LiveMonitor
	webhooks   WebhookDeliverer
	originator CallOriginator
	// portalBaseURL is the public origin for invite/reset links we email.
	portalBaseURL string
	// Phase 5.1: SIP server connection info displayed to users for softphone
	// setup. Falls back to portal host:5060/udp when unset.
	sipPublicHost      string
	sipPublicPort      int
	sipPublicTransport string
	// Phase A.1: suffix appended to tenant slug to auto-generate the
	// primary sip_domain on tenant create. Empty disables auto-gen.
	sipDomainSuffix string
	// Voicemail inbox: root under which FS-recorded audio_path values must
	// resolve before we'll stream them (path-traversal guard).
	vmStorageRoot string
	// Call recordings: root under which CDR recording_path values must resolve.
	recordingRoot string
	// sipRoutingTarget: Kamailio host:port used as fs_path for portal-originated
	// member calls (paging broadcast). Empty disables broadcast.
	sipRoutingTarget string
	// Secure controls cookie Secure flag — set true behind HTTPS.
	Secure bool
}

// Mailer is the SMTP sender the invite + reset + verify email flows need.
// nil → skip silently (dev environments).
type Mailer interface {
	SendInvite(to, inviterName, tenantName, acceptURL string) error
	SendPasswordReset(to, resetURL string) error
	SendEmailVerification(to, verifyURL string) error
}

// Options for portal.New. All fields optional.
type Options struct {
	Mailer        Mailer
	PortalBaseURL string
	Audit         *audit.Logger
	Sealer        *crypto.Sealer   // for TOTP secret seal/open; nil disables 2FA enrollment
	SSO           *sso.Manager     // for OIDC provider discovery + token exchange
	SAMLKey       *sso.SAMLKeypair // shared SP keypair; nil disables SAML routes
	GatewaySyncer GatewaySyncer    // Phase 5.1: rewrites FS gateway XML + sofia rescan
	LiveMonitor   LiveMonitor      // live call view: active calls + hangup via ESL
	Webhooks      WebhookDeliverer // outbound webhook delivery (for the test button)
	Originator    CallOriginator   // Phase 5.1: FS originate for test-call buttons

	// Phase 5.1: SIP server connection info to display on extension credential
	// pages so users can configure their softphone/desk phone.
	SIPPublicHost      string
	SIPPublicPort      int
	SIPPublicTransport string

	// Phase A.1: tenant slug + this suffix → primary sip_domain on create.
	// e.g. "pbx.tendpos.com" → tenant "bbh" gets bbh.pbx.tendpos.com.
	SIPDomainSuffix string

	// Voicemail inbox: root under which recorded audio files must live for
	// the portal to stream them. Empty disables voicemail audio streaming.
	VoicemailStorageRoot string
	// Call recordings root. Empty disables recording streaming.
	RecordingStorageRoot string
	// SIPRoutingTarget is the internal host:port (Kamailio) used as fs_path
	// when the portal originates calls straight to a member's registration —
	// e.g. the paging broadcast (recorded page). Empty disables broadcast.
	SIPRoutingTarget string
}

func New(s *store.Store, opts Options) (*Server, error) {
	if opts.SIPPublicPort == 0 {
		opts.SIPPublicPort = 5060
	}
	if opts.SIPPublicTransport == "" {
		opts.SIPPublicTransport = "udp"
	}
	srv := &Server{
		store:              s,
		mailer:             opts.Mailer,
		portalBaseURL:      opts.PortalBaseURL,
		audit:              opts.Audit,
		sealer:             opts.Sealer,
		ssoMgr:             opts.SSO,
		samlKey:            opts.SAMLKey,
		gwSyncer:           opts.GatewaySyncer,
		live:               opts.LiveMonitor,
		webhooks:           opts.Webhooks,
		originator:         opts.Originator,
		sipPublicHost:      opts.SIPPublicHost,
		sipPublicPort:      opts.SIPPublicPort,
		sipPublicTransport: opts.SIPPublicTransport,
		sipDomainSuffix:    strings.TrimPrefix(opts.SIPDomainSuffix, "."),
		vmStorageRoot:      opts.VoicemailStorageRoot,
		recordingRoot:      opts.RecordingStorageRoot,
		sipRoutingTarget:   opts.SIPRoutingTarget,
		// Auto-detect cookie Secure flag from PortalBaseURL — when we're
		// served behind https://, set the flag so browsers refuse to
		// send the session cookie over plaintext.
		Secure: strings.HasPrefix(opts.PortalBaseURL, "https://"),
	}
	t := template.New("").Funcs(template.FuncMap{
		"deref":       funcs["deref"],
		"dyntemplate": srv.dyntemplate,
		"humandur":    humandur,
		"insightFor":  insightFor,
	})
	if _, err := t.ParseFS(tmplFS, "templates/*.html"); err != nil {
		return nil, err
	}
	srv.tmpls = t
	return srv, nil
}

// dyntemplate renders a named template into a string, returning template.HTML
// so the layout doesn't HTML-escape it. Used by layout.html for
// {{dyntemplate .ContentName .}}.
func (s *Server) dyntemplate(name string, data any) (template.HTML, error) {
	var buf bytes.Buffer
	if err := s.tmpls.ExecuteTemplate(&buf, name, data); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	// Served stylesheet (public — the login page needs it too).
	r.Get("/static/app.css", func(w http.ResponseWriter, _ *http.Request) {
		b, err := staticFS.ReadFile("static/app.css")
		if err != nil {
			http.Error(w, "not found", 404)
			return
		}
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(b)
	})

	// Broadcast PWA assets (public, non-sensitive: JS/manifest/icon/service
	// worker). Served unauthenticated so install + the service worker fetch
	// don't bounce through the login redirect. The page itself (/broadcast)
	// and the upload endpoint stay behind auth.
	servePortalStatic := func(path, file, ctype string, extra map[string]string) {
		r.Get(path, func(w http.ResponseWriter, _ *http.Request) {
			b, err := staticFS.ReadFile(file)
			if err != nil {
				http.Error(w, "not found", 404)
				return
			}
			w.Header().Set("Content-Type", ctype)
			w.Header().Set("Cache-Control", "no-cache")
			for k, v := range extra {
				w.Header().Set(k, v)
			}
			_, _ = w.Write(b)
		})
	}
	servePortalStatic("/static/broadcast.js", "static/broadcast.js", "application/javascript; charset=utf-8", nil)
	servePortalStatic("/static/broadcast-icon.svg", "static/broadcast-icon.svg", "image/svg+xml", nil)
	servePortalStatic("/broadcast.manifest.webmanifest", "static/broadcast.manifest.webmanifest", "application/manifest+json", nil)
	// The service worker is served at the app root so its default scope covers
	// /admin/broadcast; Service-Worker-Allowed widens it to the /admin base.
	servePortalStatic("/broadcast.sw.js", "static/broadcast.sw.js", "application/javascript; charset=utf-8", map[string]string{"Service-Worker-Allowed": "/admin/"})

	r.Get("/login", s.handleLogin)
	r.Post("/login", s.handleLoginPost)
	r.Get("/logout", s.handleLogout)

	// Phase 4.5: public password reset + invite acceptance.
	r.Get("/forgot-password", s.handleForgotPasswordGet)
	r.Post("/forgot-password", s.handleForgotPasswordPost)
	r.Get("/reset-password/{token}", s.handleResetPasswordGet)
	r.Post("/reset-password/{token}", s.handleResetPasswordPost)
	r.Get("/accept-invite/{token}", s.handleAcceptInviteGet)
	r.Post("/accept-invite/{token}", s.handleAcceptInvitePost)

	// Phase 4.7: 2FA challenge page — public-ish; gated by sip_2fa_pending cookie.
	r.Get("/2fa-challenge", s.twoFAChallengeGet)
	r.Post("/2fa-challenge", s.twoFAChallengePost)

	// Phase 4.8: SSO public entry points + callback.
	r.Post("/login/sso", s.handleSSOLoginByEmail)
	r.Get("/sso/{tenantSlug}/login", s.handleSSOLoginByTenant)
	r.Get("/sso/callback", s.handleSSOCallback)

	// Phase 4.10: SAML public entry points.
	r.Post("/login/saml", s.handleSAMLLoginByEmail)
	r.Get("/sso/saml/metadata", s.samlMetadata)
	r.Get("/sso/saml/{tenantSlug}/login", s.handleSAMLLoginByTenant)
	r.Post("/sso/saml/callback", s.handleSAMLCallback)

	// Phase 4.9: verify-email entry points. Consume + landing are public;
	// resend needs a session (handled inside its own auth check).
	r.Get("/verify-email/sent", s.verifyEmailSent)
	r.Get("/verify-email/{token}", s.verifyEmailGet)

	// Authenticated routes.
	r.Group(func(r chi.Router) {
		r.Use(s.authRequired)
		r.Use(s.require2FAEnrollment) // Phase 4.7 grace-mode enforcer
		r.Use(s.adminScopeRequired)   // confine non-admins to /me (self-service)
		r.Get("/", s.dashboard)
		// Self-service portal (owner-scoped; available to any authenticated user).
		r.Get("/me", s.meHome)
		r.Get("/me/extensions/{extensionID}", s.meExtension)
		r.Post("/me/extensions/{extensionID}/features", s.meFeaturesUpdate)
		r.Get("/me/extensions/{extensionID}/voicemail/{msgID}/audio", s.meVoicemailAudio)
		r.Post("/me/extensions/{extensionID}/voicemail/{msgID}/delete", s.meVoicemailDelete)
		r.Get("/ops/live", s.opsLive)
		r.Get("/ops/live/fragment", s.opsLiveFragment)
		r.Post("/switch-tenant", s.switchTenant)
		r.Post("/tenants", s.createTenant)
		r.Get("/tenants/{tenantID}", s.tenantDetail)
		r.Get("/tenants/{tenantID}/sms", s.smsConversations)
		r.Get("/tenants/{tenantID}/sms/thread", s.smsThread)
		r.Post("/tenants/{tenantID}/sms/send", s.smsSend)
		r.Get("/tenants/{tenantID}/reports", s.reportsView)
		r.Get("/tenants/{tenantID}/analytics", s.tenantAnalytics)
		r.Get("/tenants/{tenantID}/cdrs", s.tenantCDRs)
		r.Get("/tenants/{tenantID}/cdrs.csv", s.tenantCDRsCSV)
		r.Post("/tenants/{tenantID}/cdrs/{cdrID}/note", s.tenantCDRNote)
		r.Post("/tenants/{tenantID}/cdrs/{cdrID}/disposition", s.tenantCDRDisposition)
		r.Get("/tenants/{tenantID}/cdrs/{cdrID}/recording", s.cdrRecordingAudio)
		r.Post("/tenants/{tenantID}/cdrs/{cdrID}/recording/delete", s.cdrRecordingDelete)
		r.Post("/tenants/{tenantID}/sip-domains", s.createSIPDomain)
		r.Post("/tenants/{tenantID}/moh", s.tenantSetMoH)
		r.Get("/tenants/{tenantID}/extensions.csv", s.tenantExtensionsCSV)
		r.Post("/tenants/{tenantID}/extensions", s.createExtension)
		r.Post("/tenants/{tenantID}/extensions/bulk", s.bulkCreateExtensions)
		r.Post("/tenants/{tenantID}/devices", s.createDevice)
		r.Get("/tenants/{tenantID}/devices/{mac}", s.deviceDetail)
		r.Post("/tenants/{tenantID}/devices/{mac}/lines", s.deviceLineAdd)
		r.Post("/tenants/{tenantID}/devices/{mac}/lines/{lineID}/delete", s.deviceLineRemove)
		r.Post("/tenants/{tenantID}/devices/{mac}/delete", s.deviceDelete)
		r.Post("/tenants/{tenantID}/ring-groups", s.createRingGroup)
		r.Get("/tenants/{tenantID}/ring-groups/{rgID}", s.ringGroupDetail)
		r.Post("/tenants/{tenantID}/ring-groups/{rgID}/members", s.ringGroupMemberAdd)
		r.Post("/tenants/{tenantID}/ring-groups/{rgID}/members/{memberID}/delete", s.ringGroupMemberRemove)
		r.Post("/tenants/{tenantID}/ring-groups/{rgID}/edit", s.ringGroupEdit)
		r.Post("/tenants/{tenantID}/ring-groups/{rgID}/toggle", s.ringGroupToggle)
		r.Post("/tenants/{tenantID}/ring-groups/{rgID}/delete", s.ringGroupDelete)
		r.Post("/tenants/{tenantID}/ivrs", s.createIVR)
		r.Get("/tenants/{tenantID}/ivrs/{ivrID}", s.ivrDetail)
		r.Post("/tenants/{tenantID}/ivrs/{ivrID}/options", s.ivrOptionAdd)
		r.Post("/tenants/{tenantID}/ivrs/{ivrID}/options/{optID}/delete", s.ivrOptionRemove)
		r.Post("/tenants/{tenantID}/ivrs/{ivrID}/edit", s.ivrEdit)
		r.Post("/tenants/{tenantID}/ivrs/{ivrID}/toggle", s.ivrToggle)
		r.Post("/tenants/{tenantID}/ivrs/{ivrID}/delete", s.ivrDelete)
		r.Post("/tenants/{tenantID}/queues", s.createQueue)
		r.Get("/tenants/{tenantID}/queues/{queueID}", s.queueDetail)
		r.Post("/tenants/{tenantID}/queues/{queueID}/agents", s.queueAgentAdd)
		r.Post("/tenants/{tenantID}/queues/{queueID}/agents/{agentID}/delete", s.queueAgentRemove)
		r.Post("/tenants/{tenantID}/queues/{queueID}/edit", s.queueEdit)
		r.Post("/tenants/{tenantID}/queues/{queueID}/toggle", s.queueToggle)
		r.Post("/tenants/{tenantID}/queues/{queueID}/delete", s.queueDelete)

		r.Get("/extensions/{extensionID}", s.extensionDetail)
		r.Post("/extensions/{extensionID}/features", s.extensionFeaturesUpdate)
		r.Post("/extensions/{extensionID}/rename", s.extensionRename)
		r.Post("/extensions/{extensionID}/owner", s.extensionSetOwner)
		r.Post("/extensions/{extensionID}/delete", s.extensionDelete)
		r.Post("/extensions/{extensionID}/rotate-password", s.extensionRotatePassword)
		r.Post("/extensions/{extensionID}/voicemail", s.extensionVoicemailCreate)
		r.Get("/extensions/{extensionID}/voicemail/messages/{msgID}/audio", s.voicemailMessageAudio)
		r.Post("/extensions/{extensionID}/voicemail/messages/{msgID}/delete", s.voicemailMessageDelete)

		r.Get("/api-tokens", s.apiTokensList)
		r.Post("/api-tokens", s.apiTokensCreate)
		r.Post("/api-tokens/{tokenID}/revoke", s.apiTokensRevoke)

		// Phase 4.5: tenant-scoped invites admin page.
		r.Get("/tenants/{tenantID}/invites", s.invitesList)
		r.Post("/tenants/{tenantID}/invites", s.invitesCreate)
		r.Post("/invites/{inviteID}/revoke", s.invitesRevoke)

		// Phase 4.6: per-tenant audit log + verification gate toggle.
		r.Get("/tenants/{tenantID}/live", s.liveDashboard)
		r.Get("/tenants/{tenantID}/live/fragment", s.liveFragment)
		r.Post("/tenants/{tenantID}/live/hangup", s.liveHangup)
		r.Post("/tenants/{tenantID}/live/eavesdrop", s.liveEavesdrop)
		// Full-screen wallboard (active calls + queues + presence on one screen).
		r.Get("/tenants/{tenantID}/wallboard", s.wallboard)
		r.Get("/tenants/{tenantID}/wallboard/fragment", s.wallboardFragment)
		r.Get("/tenants/{tenantID}/audit", s.auditList)
		r.Get("/tenants/{tenantID}/audit.csv", s.tenantAuditCSV)
		r.Get("/tenants/{tenantID}/api-keys", s.tenantAPIKeysList)
		r.Post("/tenants/{tenantID}/api-keys", s.tenantAPIKeyCreate)
		r.Post("/tenants/{tenantID}/api-keys/{id}/revoke", s.tenantAPIKeyRevoke)
		r.Get("/tenants/{tenantID}/contacts", s.contactsList)
		r.Get("/tenants/{tenantID}/contacts.csv", s.contactsCSV)
		r.Post("/tenants/{tenantID}/contacts", s.contactCreate)
		r.Post("/tenants/{tenantID}/contacts/{id}/delete", s.contactDelete)
		r.Post("/tenants/{tenantID}/click-to-dial", s.clickToDial)
		r.Get("/tenants/{tenantID}/paging", s.pagingList)
		r.Post("/tenants/{tenantID}/paging", s.pagingCreate)
		r.Post("/tenants/{tenantID}/paging/{id}/delete", s.pagingDelete)
		r.Post("/tenants/{tenantID}/paging/{id}/toggle", s.pagingToggle)
		r.Post("/tenants/{tenantID}/paging/{id}/members", s.pagingMemberAdd)
		r.Post("/tenants/{tenantID}/paging/{id}/members/{memberID}/delete", s.pagingMemberRemove)
		r.Get("/tenants/{tenantID}/presence", s.presenceList)
		r.Get("/tenants/{tenantID}/conferences", s.conferenceList)
		r.Post("/tenants/{tenantID}/conferences", s.conferenceCreate)
		r.Post("/tenants/{tenantID}/conferences/{id}/delete", s.conferenceDelete)
		r.Post("/tenants/{tenantID}/conferences/{id}/toggle", s.conferenceToggle)
		r.Get("/tenants/{tenantID}/park", s.parkList)
		r.Post("/tenants/{tenantID}/park", s.parkCreate)
		r.Post("/tenants/{tenantID}/park/{id}/delete", s.parkDelete)
		r.Post("/tenants/{tenantID}/park/{id}/toggle", s.parkToggle)
		r.Get("/tenants/{tenantID}/callbacks", s.queueCallbackList)
		r.Post("/tenants/{tenantID}/callbacks/{id}/dial", s.queueCallbackDial)
		r.Post("/tenants/{tenantID}/callbacks/{id}/cancel", s.queueCallbackCancel)
		r.Get("/tenants/{tenantID}/e911", s.e911List)
		r.Post("/tenants/{tenantID}/e911", s.e911Create)
		r.Post("/tenants/{tenantID}/e911/assign", s.e911Assign)
		r.Post("/tenants/{tenantID}/e911/{id}/delete", s.e911Delete)
		r.Post("/tenants/{tenantID}/e911/{id}/toggle", s.e911Toggle)
		r.Get("/tenants/{tenantID}/blocked-numbers", s.blockedNumberList)
		r.Post("/tenants/{tenantID}/blocked-numbers", s.blockedNumberCreate)
		r.Post("/tenants/{tenantID}/blocked-numbers/{id}/delete", s.blockedNumberDelete)
		r.Get("/tenants/{tenantID}/disposition-codes", s.dispositionCodeList)
		r.Post("/tenants/{tenantID}/disposition-codes", s.dispositionCodeCreate)
		r.Post("/tenants/{tenantID}/disposition-codes/{id}/toggle", s.dispositionCodeToggle)
		r.Post("/tenants/{tenantID}/disposition-codes/{id}/delete", s.dispositionCodeDelete)
		r.Get("/tenants/{tenantID}/setup", s.tenantSetupCheck)
		r.Get("/tenants/{tenantID}/queues-live", s.queueBoard)
		r.Get("/tenants/{tenantID}/queues-live/fragment", s.queueBoardFragment)
		r.Get("/tenants/{tenantID}/webhooks", s.webhooksList)
		r.Post("/tenants/{tenantID}/webhooks", s.webhookCreate)
		r.Post("/tenants/{tenantID}/webhooks/{id}/delete", s.webhookDelete)
		r.Post("/tenants/{tenantID}/webhooks/{id}/test", s.webhookTest)
		r.Post("/tenants/{tenantID}/webhooks/{id}/toggle", s.webhookToggle)
		r.Post("/tenants/{tenantID}/webhooks/{id}/rotate-secret", s.webhookRotateSecret)
		r.Post("/tenants/{tenantID}/security", s.tenantSecurityUpdate)
		r.Post("/tenants/{tenantID}/alert-email", s.tenantAlertEmailUpdate)
		r.Post("/tenants/{tenantID}/digest", s.tenantDigestToggle)

		// Phase 4.7: 2FA enrollment + management + admin reset.
		r.Get("/security/account", s.accountPage)
		r.Post("/security/account/profile", s.accountProfilePost)
		r.Post("/security/account/password", s.accountPasswordPost)
		r.Get("/security/2fa", s.twoFAStatus)
		r.Post("/security/2fa/setup", s.twoFASetupPost)
		r.Post("/security/2fa/confirm", s.twoFAConfirmPost)
		r.Post("/security/2fa/disable", s.twoFADisablePost)
		r.Post("/security/devices/{deviceID}/revoke", s.trustedDeviceRevoke)
		r.Post("/users/{userID}/2fa/reset", s.admin2FAReset)

		// Phase 4.8: per-tenant SSO admin + per-user identity management.
		r.Get("/tenants/{tenantID}/sso", s.tenantSSOGet)
		r.Post("/tenants/{tenantID}/sso", s.tenantSSOSave)
		r.Post("/tenants/{tenantID}/sso/disable", s.tenantSSODisable)
		r.Post("/tenants/{tenantID}/sso/test", s.tenantSSOTest)
		r.Post("/tenants/{tenantID}/sso/domains", s.tenantSSODomainAdd)
		r.Post("/tenants/{tenantID}/sso/domains/{domainID}/remove", s.tenantSSODomainRemove)

		r.Get("/security/sso", s.userSSOIdentities)
		r.Post("/security/sso/{identityID}/unlink", s.userSSOUnlink)

		// Phase 4.10: tenant SAML admin.
		r.Get("/tenants/{tenantID}/saml", s.tenantSAMLGet)
		r.Post("/tenants/{tenantID}/saml", s.tenantSAMLSave)
		r.Post("/tenants/{tenantID}/saml/disable", s.tenantSAMLDisable)

		// Phase 4.11: per-user active session list.
		r.Get("/security/sessions", s.sessionsList)
		r.Post("/security/sessions/{tokenID}/revoke", s.sessionRevoke)
		r.Post("/security/sessions/revoke-all", s.sessionRevokeAll)

		// Phase 5.0: WebRTC softphone.
		r.Get("/softphone", s.softphoneGet)
		r.Post("/softphone/credentials", s.softphoneCredentialsPost)

		// Paging broadcast PWA (recorded "voice blast" to a paging group).
		r.Get("/broadcast", s.broadcastConsole)
		r.Post("/broadcast/send", s.broadcastSend)

		// In-app Help Center (rendered knowledgebase).
		r.Get("/help", s.helpIndex)
		r.Get("/help/{slug}", s.helpArticle)

		// Phase 5.1: per-tenant carrier trunks (CallCentric, Telnyx, ...).
		r.Get("/tenants/{tenantID}/trunks", s.trunksList)
		r.Post("/tenants/{tenantID}/trunks", s.trunkCreate)
		r.Post("/tenants/{tenantID}/trunks/{accountID}", s.trunkUpdate)
		r.Post("/tenants/{tenantID}/trunks/{accountID}/delete", s.trunkDelete)
		r.Get("/tenants/{tenantID}/trunks/{accountID}/status", s.trunkStatusFragment)
		r.Post("/tenants/{tenantID}/trunks/{accountID}/test-outbound", s.trunkTestOutbound)

		// Phase 5.1: per-tenant DIDs (inbound number → extension routing).
		r.Get("/tenants/{tenantID}/dids", s.didsList)
		r.Post("/tenants/{tenantID}/dids", s.didCreate)
		r.Post("/tenants/{tenantID}/dids/{didID}/edit", s.didEdit)
		r.Post("/tenants/{tenantID}/dids/{didID}/delete", s.didDelete)
		r.Post("/tenants/{tenantID}/dids/{didID}/toggle", s.didToggleEnabled)
		r.Post("/tenants/{tenantID}/dids/{didID}/test-inbound", s.didTestInbound)
		r.Post("/tenants/{tenantID}/dids/{didID}/schedule", s.didSetSchedule)
		r.Get("/tenants/{tenantID}/schedules", s.schedulesList)
		r.Post("/tenants/{tenantID}/schedules", s.scheduleCreate)
		r.Get("/tenants/{tenantID}/schedules/{schedID}", s.scheduleDetail)
		r.Post("/tenants/{tenantID}/schedules/{schedID}/delete", s.scheduleDelete)
		r.Post("/tenants/{tenantID}/schedules/{schedID}/periods", s.schedulePeriodAdd)
		r.Post("/tenants/{tenantID}/schedules/{schedID}/periods/{periodID}/delete", s.schedulePeriodRemove)
		r.Post("/tenants/{tenantID}/schedules/{schedID}/holidays", s.scheduleHolidayAdd)
		r.Post("/tenants/{tenantID}/schedules/{schedID}/holidays/{holidayID}/delete", s.scheduleHolidayRemove)
		r.Get("/tenants/{tenantID}/outbound-routes", s.outboundRoutesList)
		r.Post("/tenants/{tenantID}/outbound-routes", s.outboundRouteCreate)
		r.Post("/tenants/{tenantID}/outbound-routes/{routeID}/delete", s.outboundRouteDelete)
		r.Post("/tenants/{tenantID}/outbound-routes/{routeID}/toggle", s.outboundRouteToggle)

		// Phase 4.9: authenticated resend.
		r.Post("/verify-email/resend", s.verifyEmailResend)
	})
	return r
}

// ---------------------------------------------------------------------------
// Auth (cookie-based — wraps API token verification)
// ---------------------------------------------------------------------------

type ctxKey int

const ctxKeyToken ctxKey = iota

func (s *Server) authRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil || c.Value == "" {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		tok, err := s.store.VerifyAPIToken(r.Context(), c.Value)
		if err != nil {
			http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/admin", MaxAge: -1})
			http.Redirect(w, r, "/admin/login?flash=session+expired", http.StatusSeeOther)
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyToken, tok)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func tokenFromCtx(ctx context.Context) *store.APIToken {
	if v, ok := ctx.Value(ctxKeyToken).(*store.APIToken); ok {
		return v
	}
	return nil
}

// selfServicePrefixes are the authed paths a non-admin (member) may reach.
// Everything else under the authed group is admin-only. Paths are relative to
// the /admin mount (StripPrefix already removed it).
var selfServicePrefixes = []string{"/me", "/security", "/softphone", "/broadcast", "/help", "/switch-tenant", "/verify-email", "/logout"}

// adminScopeRequired confines non-admin sessions to the self-service area. A
// token with "admin" scope (super_admin / tenant_admin) passes through to all
// admin routes; any lower-scoped session is allowed only the self-service +
// account paths above and is otherwise redirected to /me. This both powers the
// self-service portal and closes the prior gap where any member could reach
// tenant-management routes. Must run after authRequired (needs the token).
func (s *Server) adminScopeRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := tokenFromCtx(r.Context())
		if tok != nil && tok.Scope == "admin" {
			next.ServeHTTP(w, r)
			return
		}
		p := r.URL.Path
		for _, pre := range selfServicePrefixes {
			if p == pre || strings.HasPrefix(p, pre+"/") {
				next.ServeHTTP(w, r)
				return
			}
		}
		http.Redirect(w, r, "/admin/me", http.StatusSeeOther)
	})
}

// require2FAEnrollment is the per-tenant `require_2fa` grace-mode enforcer.
// If any of the signed-in user's tenants requires 2FA and they haven't
// enrolled yet, redirect them to /admin/security/2fa. Carve-outs:
//   - the 2FA pages themselves (so they can actually enroll)
//   - the logout endpoint
//   - any /security/* page (devices, recovery codes, etc.)
//   - super-admins (no enforcement against them)
func (s *Server) require2FAEnrollment(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasPrefix(p, "/security/") ||
			p == "/security" || p == "/logout" || p == "/switch-tenant" {
			next.ServeHTTP(w, r)
			return
		}
		tok := tokenFromCtx(r.Context())
		if tok == nil {
			next.ServeHTTP(w, r)
			return
		}
		user, _ := s.userFromSessionToken(r.Context(), tok)
		if user == nil || user.Role == "super_admin" {
			next.ServeHTTP(w, r)
			return
		}
		n, _ := s.store.CountConfirmedTwoFAMethods(r.Context(), user.ID)
		if n > 0 {
			next.ServeHTTP(w, r)
			return
		}
		memberships, _ := s.store.ListMembershipsForUser(r.Context(), user.ID)
		for _, m := range memberships {
			if req, _ := s.store.TenantRequires2FA(r.Context(), m.TenantID); req {
				http.Redirect(w, r, "/admin/security/2fa?flash=Your+workspace+requires+2FA.+Enroll+below+to+continue.", http.StatusSeeOther)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	flash := r.URL.Query().Get("flash")
	s.render(w, "login", map[string]any{"Flash": flash})
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.render(w, "login", map[string]any{"Flash": "bad form"})
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	token := strings.TrimSpace(r.FormValue("token"))

	ip, ua := audit.FromRequest(r)

	switch {
	case email != "" && password != "":
		// Phase 4.11: rate-limit per email + per IP before doing any
		// password work. Bcrypt is expensive — refusing rapidly here saves
		// CPU as well as deterring brute-force.
		if limited, msg := s.store.LoginRateLimited(r.Context(), email, ip); limited {
			s.audit.Log(r.Context(), audit.Event{
				ActorEmail: email, Event: "auth.login.rate_limited",
				IPAddress: ip, UserAgent: ua,
			})
			s.render(w, "login", map[string]any{"Flash": msg})
			return
		}

		u, err := s.store.VerifyUserPassword(r.Context(), email, password)
		if err != nil {
			s.store.RecordLoginFailure(r.Context(), email, ip)
			s.audit.Log(r.Context(), audit.Event{
				ActorEmail: email, Event: audit.EventLoginFailure,
				IPAddress: ip, UserAgent: ua,
				Payload: map[string]any{"reason": "invalid_credentials"},
			})
			s.render(w, "login", map[string]any{"Flash": "Invalid email or password."})
			return
		}
		// Successful password verify — clear the per-email failure counter.
		s.store.ResetLoginCounters(r.Context(), email)
		// Phase 4.4: multi-tenant memberships. Mint a session that's
		// either NULL-tenant (super-admin) or pinned to the first
		// membership; the dashboard offers a picker if >1.
		memberships, _ := s.store.ListMembershipsForUser(r.Context(), u.ID)

		// Phase 4.6: per-tenant email verification gate. If *any* of the
		// user's tenants requires verified email, block login until they
		// verify. Super-admins are exempt.
		if u.Role != "super_admin" && u.EmailVerifiedAt == nil {
			for _, m := range memberships {
				if req, _ := s.store.TenantRequiresEmailVerified(r.Context(), m.TenantID); req {
					s.audit.Log(r.Context(), audit.Event{
						TenantID: &m.TenantID, ActorUserID: &u.ID, ActorEmail: u.Email,
						Event:     audit.EventLoginBlockedUnverified,
						IPAddress: ip, UserAgent: ua,
					})
					s.render(w, "login", map[string]any{
						"Flash": "Please verify your email before signing in. Check your inbox for the verification link, or ask an admin to resend it.",
					})
					return
				}
			}
		}

		// Phase 4.8: SSO enforcement. If any of the user's tenants requires
		// SSO, password login is rejected — they must come in through the
		// IdP. Super-admins exempt so a misconfigured SSO can't lock the
		// platform out.
		if u.Role != "super_admin" {
			for _, m := range memberships {
				if req, _ := s.store.TenantRequiresSSO(r.Context(), m.TenantID); req {
					s.audit.Log(r.Context(), audit.Event{
						TenantID: &m.TenantID, ActorUserID: &u.ID, ActorEmail: u.Email,
						Event:     "auth.login.blocked_sso",
						IPAddress: ip, UserAgent: ua,
					})
					s.render(w, "login", map[string]any{
						"Flash": "Your workspace requires single sign-on. Enter your work email below to use SSO.",
					})
					return
				}
			}
		}
		var tenantID *uuid.UUID
		scope := store.ScopeForRole(u.Role)
		switch {
		case len(memberships) == 0:
			if u.Role != "super_admin" {
				s.render(w, "login", map[string]any{"Flash": "Account has no workspaces. Contact an admin."})
				return
			}
			// super-admin: tenant_id stays nil, admin scope
		case len(memberships) == 1:
			// Auto-pin to the single workspace.
			m := memberships[0]
			tenantID = &m.TenantID
			scope = store.ScopeForRole(m.Role)
		default:
			// 2+ memberships: leave token unpinned so the dashboard
			// renders the picker. Take the broadest scope across all
			// memberships so the user can switch without re-login.
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
		// Phase 4.7: 2FA challenge interstitial. If the user has confirmed
		// methods, gateLoginWith2FA either mounts a session via trusted-
		// device skip or redirects to /admin/2fa-challenge. Either way we
		// return early; the post-challenge path mints the session + audit.
		if s.gateLoginWith2FA(w, r, u, tenantID, scope) {
			return
		}

		issued, err := s.store.CreateAPIToken(r.Context(), store.CreateAPITokenInput{
			TenantID:  tenantID,
			Name:      "portal:" + u.Email + ":" + time.Now().UTC().Format("20060102T150405"),
			Scope:     scope,
			ExpiresAt: portalSessionExpiry(),
		})
		if err != nil {
			s.render(w, "login", map[string]any{"Flash": "Internal error creating session."})
			return
		}
		s.setSessionCookie(w, issued.Plaintext)
		s.audit.Log(r.Context(), audit.Event{
			TenantID: tenantID, ActorUserID: &u.ID, ActorEmail: u.Email,
			ActorTokenID: &issued.ID,
			Event:        audit.EventLoginSuccess,
			IPAddress:    ip, UserAgent: ua,
			Payload: map[string]any{"memberships": len(memberships), "scope": scope},
		})
		http.Redirect(w, r, "/admin/", http.StatusSeeOther)
		return

	case token != "":
		tok, err := s.store.VerifyAPIToken(r.Context(), token)
		if err != nil {
			s.audit.Log(r.Context(), audit.Event{
				Event:     audit.EventLoginFailure,
				IPAddress: ip, UserAgent: ua,
				Payload: map[string]any{"reason": "invalid_token"},
			})
			s.render(w, "login", map[string]any{"Flash": "Invalid token."})
			return
		}
		s.setSessionCookie(w, token)
		s.audit.Log(r.Context(), audit.Event{
			TenantID: tok.TenantID, ActorTokenID: &tok.ID,
			Event:     audit.EventLoginSuccess,
			IPAddress: ip, UserAgent: ua,
			Payload: map[string]any{"method": "token"},
		})
		http.Redirect(w, r, "/admin/", http.StatusSeeOther)
		return

	default:
		s.render(w, "login", map[string]any{"Flash": "Enter an email + password, or an API token."})
	}
}

// portalSessionTTL bounds how long a freshly-minted portal session token
// is valid. The cookie itself purges from the browser after 7 days, but
// the token row in api_tokens needs its own server-side expiry so a
// captured cookie value can't be replayed indefinitely.
//
// 30 days is a deliberate compromise: long enough that day-to-day use
// doesn't require frequent re-auth, short enough that a leaked cookie
// has a bounded blast radius. Tenants that require tighter control can
// still call the revoke endpoint at /admin/security/sessions.
const portalSessionTTL = 30 * 24 * time.Hour

// portalSessionExpiry returns a *time.Time pointing at now + portalSessionTTL,
// suitable for the ExpiresAt field of store.CreateAPITokenInput.
func portalSessionExpiry() *time.Time {
	t := time.Now().Add(portalSessionTTL)
	return &t
}

func (s *Server) setSessionCookie(w http.ResponseWriter, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    value,
		Path:     "/admin",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.Secure,
		MaxAge:   86400 * 7,
	})
}

// Phase 4.4 tenant switcher ----------------------------------------------
//
// Self-service signup was removed (audit C3): /admin/signup was a public
// route anyone could POST to in order to create a tenant + admin user +
// admin-scope API token. Tenants are now created by the super-admin via
// /admin/tenants, and tenant admins onboard users via the invite flow at
// /admin/accept-invite/{token}. CreateTenantWithAdmin in the store layer
// is retained because super-admins call it through the tenant-create
// handler.

func (s *Server) switchTenant(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	tid, err := uuid.Parse(r.FormValue("tenant_id"))
	if err != nil {
		http.Error(w, "bad tenant_id", 400)
		return
	}
	tok := tokenFromCtx(r.Context())
	if tok == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	user, _ := s.userFromSessionToken(r.Context(), tok)
	if user == nil {
		http.Redirect(w, r, "/admin/", http.StatusSeeOther)
		return
	}
	m, err := s.store.GetMembership(r.Context(), user.ID, tid)
	if err != nil {
		http.Error(w, "you don't belong to that tenant", http.StatusForbidden)
		return
	}
	if strings.HasPrefix(tok.Name, "portal:") {
		_ = s.store.RevokeAPIToken(r.Context(), tok.ID)
	}
	issued, err := s.store.CreateAPIToken(r.Context(), store.CreateAPITokenInput{
		TenantID: &m.TenantID,
		Name:     "portal:" + user.Email + ":" + time.Now().UTC().Format("20060102T150405"),
		Scope:    store.ScopeForRole(m.Role),
	})
	if err != nil {
		http.Error(w, "failed to switch", 500)
		return
	}
	s.setSessionCookie(w, issued.Plaintext)
	http.Redirect(w, r, "/admin/tenants/"+tid.String(), http.StatusSeeOther)
}

func (s *Server) userFromSessionToken(ctx context.Context, tok *store.APIToken) (*store.User, error) {
	if !strings.HasPrefix(tok.Name, "portal:") {
		return nil, nil
	}
	rest := tok.Name[len("portal:"):]
	colon := strings.Index(rest, ":")
	if colon == -1 {
		return nil, nil
	}
	return s.store.GetUserByEmail(ctx, rest[:colon])
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	ip, ua := audit.FromRequest(r)
	// Revoke the session token (if the cookie carries one we issued via
	// email-password login). Tokens pasted in directly stay valid — those
	// belong to the user, not the session.
	if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
		if tok, err := s.store.VerifyAPIToken(r.Context(), c.Value); err == nil {
			if strings.HasPrefix(tok.Name, "portal:") {
				_ = s.store.RevokeAPIToken(r.Context(), tok.ID)
			}
			s.audit.Log(r.Context(), audit.Event{
				TenantID: tok.TenantID, ActorTokenID: &tok.ID,
				Event: audit.EventLogout, IPAddress: ip, UserAgent: ua,
			})
		}
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/admin", MaxAge: -1})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Pages
// ---------------------------------------------------------------------------

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	tenants, err := s.store.ListTenants(r.Context())
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	if tok := tokenFromCtx(r.Context()); tok != nil && tok.TenantID != nil {
		// Tenant-scoped token: filter to just its tenant.
		var filtered []store.Tenant
		for _, t := range tenants {
			if t.ID == *tok.TenantID {
				filtered = append(filtered, t)
			}
		}
		tenants = filtered
	}
	// Optional workspace filter (?q=) — substring match on name or slug.
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	if search != "" {
		needle := strings.ToLower(search)
		var matched []store.Tenant
		for _, t := range tenants {
			if strings.Contains(strings.ToLower(t.Name), needle) || strings.Contains(strings.ToLower(t.Slug), needle) {
				matched = append(matched, t)
			}
		}
		tenants = matched
	}
	var scope *uuid.UUID
	if tok := tokenFromCtx(r.Context()); tok != nil && tok.TenantID != nil {
		scope = tok.TenantID
	}
	stats, err := s.store.GetPlatformStats(r.Context(), scope)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	recent, _ := s.store.ListRecentCDRs(r.Context(), scope, 10)
	counts, _ := s.store.GetTenantCounts(r.Context())
	s.renderLayout(w, r, "Tenants", "dashboard", map[string]any{
		"Tenants":     tenants,
		"Stats":       stats,
		"RecentCalls": recent,
		"Counts":      counts,
		"Search":      search,
	})
}

func (s *Server) createTenant(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.errPage(w, r, err)
		return
	}
	slug := r.FormValue("slug")
	name := r.FormValue("name")
	t, err := s.store.CreateTenant(r.Context(), slug, name)
	if err != nil {
		s.flashErr(w, r, "/admin/", err)
		return
	}
	// Phase A.1: auto-create the tenant's primary sip_domain so the
	// operator never has to think about it. Skipped when
	// SIPDomainSuffix is empty (dev / pre-DNS deployments).
	if s.sipDomainSuffix != "" {
		domain := t.Slug + "." + s.sipDomainSuffix
		if _, derr := s.store.CreateSIPDomain(r.Context(), t.ID, domain, true); derr != nil {
			// Don't fail the whole tenant create — the operator can
			// still set the domain manually on the tenant detail page.
			slog.Warn("auto-create sip_domain failed",
				"tenant", t.ID, "domain", domain, "err", derr)
		}
	}
	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

func (s *Server) tenantDetail(w http.ResponseWriter, r *http.Request) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	if !s.canAccessTenant(r.Context(), tid) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	ctx := r.Context()
	tenant, err := s.store.GetTenant(ctx, tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}

	data := map[string]any{"Tenant": tenant}

	// Pull related entities (best-effort; missing data shouldn't crash the page).
	data["Extensions"] = mustExtensions(ctx, s.store, tid)
	data["Devices"] = mustDevices(ctx, s.store, tid)

	// Phase 5.1: onboarding checklist counts so the tenant page can show
	// a "Getting started" widget for new tenants.
	trunkCount := 0
	if accts, err := s.store.ListCarrierAccountsForTenant(ctx, tid); err == nil {
		trunkCount = len(accts)
	}
	data["TrunkCount"] = trunkCount
	data["RingGroups"], _ = s.store.ListRingGroupsForTenant(ctx, tid)
	data["DIDs"], _ = s.store.ListDIDsForTenant(ctx, tid)
	domains := mustSIPDomains(ctx, s.store, tid)
	data["SIPDomains"] = domains
	if d, err := s.store.PrimaryDomainForTenant(ctx, tid); err == nil {
		data["PrimaryDomain"] = d
	}
	data["IVRs"], _ = listIVRs(ctx, s.store, tid)
	data["Queues"], _ = listQueues(ctx, s.store, tid)
	data["MoH"], _ = s.store.GetTenantMoH(ctx, tid)
	data["NavActive"] = "overview"

	s.renderLayout(w, r, tenant.Name, "tenant", data)
}

// tenantSetMoH updates a tenant's Music-on-Hold source.
func (s *Server) tenantSetMoH(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	redirect := "/admin/tenants/" + tid.String()
	if err := s.store.SetTenantMoH(r.Context(), tid, strings.TrimSpace(r.FormValue("moh_url"))); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	http.Redirect(w, r, redirect+"?flash=Music+on+hold+saved.", http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Tenant detail — create handlers (POST → 303 back to tenant page)
// ---------------------------------------------------------------------------

func (s *Server) createSIPDomain(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	if _, err := s.store.CreateSIPDomain(r.Context(), tid, r.FormValue("domain"), r.FormValue("is_primary") == "true"); err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String(), err)
		return
	}
	http.Redirect(w, r, "/admin/tenants/"+tid.String(), http.StatusSeeOther)
}

func (s *Server) createExtension(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	domainID, err := uuid.Parse(r.FormValue("sip_domain_id"))
	if err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String(), errors.New("a primary SIP domain is required"))
		return
	}
	if _, err := s.store.CreateExtension(r.Context(), tid, domainID,
		r.FormValue("extension"), "", "", r.FormValue("display_name")); err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String(), err)
		return
	}
	http.Redirect(w, r, "/admin/tenants/"+tid.String(), http.StatusSeeOther)
}

// bulkCreateExtensions creates many extensions from a pasted list, one per
// line, "<extension>[,<display name>]". SIP credentials are auto-generated.
// Partial success is fine: it reports how many were created vs skipped.
func (s *Server) bulkCreateExtensions(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	redirect := "/admin/tenants/" + tid.String()
	domainID, err := uuid.Parse(r.FormValue("sip_domain_id"))
	if err != nil {
		s.flashErr(w, r, redirect, errors.New("a primary SIP domain is required"))
		return
	}
	var created, skipped int
	var firstErr string
	for _, line := range strings.Split(r.FormValue("csv"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		ext, display := line, ""
		if i := strings.IndexAny(line, ",\t"); i >= 0 {
			ext = strings.TrimSpace(line[:i])
			display = strings.TrimSpace(line[i+1:])
		}
		if ext == "" {
			continue
		}
		if _, err := s.store.CreateExtension(r.Context(), tid, domainID, ext, "", "", display); err != nil {
			skipped++
			if firstErr == "" {
				firstErr = ext + ": " + err.Error()
			}
			continue
		}
		created++
	}
	ip, ua := audit.FromRequest(r)
	var actorTok *uuid.UUID
	if tok := tokenFromCtx(r.Context()); tok != nil {
		actorTok = &tok.ID
	}
	s.audit.Log(r.Context(), audit.Event{
		TenantID: &tid, ActorTokenID: actorTok,
		Event: "extension.bulk_created", TargetType: "tenant", TargetID: &tid,
		IPAddress: ip, UserAgent: ua,
		Payload: map[string]any{"created": created, "skipped": skipped},
	})
	msg := "Created " + strconv.Itoa(created) + ", skipped " + strconv.Itoa(skipped)
	if firstErr != "" {
		msg += " (first error — " + firstErr + ")"
	}
	http.Redirect(w, r, redirect+"?flash="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (s *Server) createDevice(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	if _, err := s.store.CreateDevice(r.Context(), tid,
		r.FormValue("mac"), r.FormValue("vendor"), r.FormValue("model"), r.FormValue("label")); err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String(), err)
		return
	}
	http.Redirect(w, r, "/admin/tenants/"+tid.String(), http.StatusSeeOther)
}

func (s *Server) createRingGroup(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	if _, err := s.store.CreateRingGroup(r.Context(), store.CreateRingGroupInput{
		TenantID: tid, Extension: r.FormValue("extension"),
		Name: r.FormValue("name"), Strategy: r.FormValue("strategy"),
	}); err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String(), err)
		return
	}
	http.Redirect(w, r, "/admin/tenants/"+tid.String(), http.StatusSeeOther)
}

func (s *Server) createIVR(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	if _, err := s.store.CreateIVR(r.Context(), store.CreateIVRInput{
		TenantID: tid, Extension: r.FormValue("extension"), Name: r.FormValue("name"),
	}); err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String(), err)
		return
	}
	http.Redirect(w, r, "/admin/tenants/"+tid.String(), http.StatusSeeOther)
}

func (s *Server) createQueue(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	if _, err := s.store.CreateQueue(r.Context(), store.CreateQueueInput{
		TenantID: tid, Extension: r.FormValue("extension"),
		Name: r.FormValue("name"), Strategy: r.FormValue("strategy"),
	}); err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String(), err)
		return
	}
	http.Redirect(w, r, "/admin/tenants/"+tid.String(), http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// CDRs
// ---------------------------------------------------------------------------

func (s *Server) tenantCDRs(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	q := r.URL.Query()
	filter := cdrFilterFromQuery(q, 200)
	cdrs, _ := s.store.ListCDRsFilteredForTenant(r.Context(), tid, filter)
	// AI insights (if any) keyed by CDR id — empty map when the feature is off
	// or nothing has been processed, so the template shows nothing extra.
	insights, _ := s.store.ListCallInsightsByCDRForTenant(r.Context(), tid, filter.Limit)
	// Active disposition codes for the per-row assignment dropdown (empty when the
	// tenant hasn't defined any — the column then renders just the assigned label).
	dispositions, _ := s.store.ListActiveDispositionCodesForTenant(r.Context(), tid)
	s.renderLayout(w, r, tenant.Name+" · CDRs", "cdrs", map[string]any{
		"Tenant":       tenant,
		"CDRs":         cdrs,
		"Insights":     insights,
		"Dispositions": dispositions,
		"Direction":    filter.Direction,
		"Search":       filter.Search,
		"Since":        q.Get("since"),
		"Until":        q.Get("until"),
		"NavActive":    "cdrs",
	})
}

// cdrFilterFromQuery builds a CDRFilter from the portal's query params. Dates
// are YYYY-MM-DD in UTC; "until" is inclusive of the chosen day (advanced 24h).
func cdrFilterFromQuery(q url.Values, limit int) store.CDRFilter {
	f := store.CDRFilter{
		Direction: q.Get("direction"),
		Search:    strings.TrimSpace(q.Get("q")),
		Limit:     limit,
	}
	if v := q.Get("since"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			f.Since = &t
		}
	}
	if v := q.Get("until"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			end := t.Add(24 * time.Hour)
			f.Until = &end
		}
	}
	return f
}

// csvSafe neutralizes spreadsheet formula injection: a cell beginning with
// = + - @ (or a leading tab/CR) is treated as a formula by Excel/Sheets. Some
// CSV fields (e.g. inbound caller-ID name) are attacker-influenced, so prefix
// such values with a single quote so they render as literal text.
func csvSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

// tenantCDRsCSV (the filtered call-log CSV export) lives in cdr_export.go.

// tenantCDRNote sets/clears a free-text note on a call record.
func (s *Server) tenantCDRNote(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	cdrID, err := uuid.Parse(chi.URLParam(r, "cdrID"))
	if err != nil {
		http.Error(w, "bad cdr id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	note := strings.TrimSpace(r.FormValue("note"))
	if err := s.store.SetCDRNote(r.Context(), tid, cdrID, note); err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/cdrs", err)
		return
	}
	s.auditNested(r, tid, "cdr.note.set", "cdr", &cdrID, nil)
	dest := r.Referer()
	if dest == "" {
		dest = "/admin/tenants/" + tid.String() + "/cdrs"
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// tenantCDRDisposition assigns or clears a tenant-defined wrap-up code on a call.
// An empty "code" value clears the assignment; a non-empty value must be a UUID of
// a code that belongs to this tenant (enforced in SetCDRDisposition's subquery).
func (s *Server) tenantCDRDisposition(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	cdrID, err := uuid.Parse(chi.URLParam(r, "cdrID"))
	if err != nil {
		http.Error(w, "bad cdr id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	dest := r.Referer()
	if dest == "" {
		dest = "/admin/tenants/" + tid.String() + "/cdrs"
	}
	var codeID *uuid.UUID
	if v := strings.TrimSpace(r.FormValue("code")); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "bad disposition code id", http.StatusBadRequest)
			return
		}
		codeID = &id
	}
	if err := s.store.SetCDRDisposition(r.Context(), tid, cdrID, codeID); err != nil {
		s.flashErr(w, r, dest, err)
		return
	}
	s.auditNested(r, tid, "cdr.disposition.set", "cdr", &cdrID, nil)
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// tenantExtensionsCSV streams the tenant's active extensions as a CSV — a
// provisioning sheet for onboarding (numbers, names, SIP usernames; no secrets).
func (s *Server) tenantExtensionsCSV(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if _, err := s.store.GetTenant(r.Context(), tid); err != nil {
		s.errPage(w, r, err)
		return
	}
	exts := mustExtensions(r.Context(), s.store, tid)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="extensions.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"extension", "display_name", "sip_username", "voicemail", "dnd", "recording"})
	yn := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	for _, e := range exts {
		_ = cw.Write([]string{csvSafe(e.Extension), csvSafe(e.DisplayName), csvSafe(e.SIPUsername), yn(e.VoicemailEnabled), yn(e.DoNotDisturb), yn(e.RecordingEnabled)})
	}
	cw.Flush()
}

// tenantAuditCSV streams the tenant's audit log as a CSV for compliance/security
// review. Mirrors the on-screen log but unpaginated (up to 10k rows).
func (s *Server) tenantAuditCSV(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if _, err := s.store.GetTenant(r.Context(), tid); err != nil {
		s.errPage(w, r, err)
		return
	}
	entries, err := s.store.ListAuditForTenant(r.Context(), tid, 10000)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="audit-log.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"created_at", "actor_email", "event", "target_type", "target_id", "ip_address", "user_agent", "payload"})
	for _, e := range entries {
		target := ""
		if e.TargetID != nil {
			target = e.TargetID.String()
		}
		_ = cw.Write([]string{
			e.CreatedAt.UTC().Format(time.RFC3339),
			csvSafe(e.ActorEmail), csvSafe(e.Event), csvSafe(e.TargetType),
			target, csvSafe(e.IPAddress), csvSafe(e.UserAgent), csvSafe(string(e.Payload)),
		})
	}
	cw.Flush()
}

// ---------------------------------------------------------------------------
// Extension detail
// ---------------------------------------------------------------------------

func (s *Server) extensionDetail(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "extensionID"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ext, err := s.lookupExtensionByID(r.Context(), id)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	if !s.canAccessTenant(r.Context(), ext.TenantID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	tenant, _ := s.store.GetTenant(r.Context(), ext.TenantID)
	vmBox, _ := s.store.GetVoicemailBoxByExtensionID(r.Context(), id)
	var vmMessages []store.VoicemailMessage
	var vmTranscripts map[uuid.UUID]string
	if vmBox != nil {
		vmMessages, _ = s.store.ListVoicemailMessagesForBox(r.Context(), vmBox.ID)
		// AI transcripts (empty map when feature off / none yet).
		vmTranscripts, _ = s.store.ListVoicemailTranscriptsForBox(r.Context(), vmBox.ID)
	}

	// Phase 5.1: SIP credentials section. Plaintext password is shown only
	// when ?reveal=1 is set (so the page is safe to share a screenshot of
	// otherwise). Domain comes from the extension's primary SIP domain.
	_, password, domain, _ := s.store.GetExtensionSIPPassword(r.Context(), id)
	reveal := r.URL.Query().Get("reveal") == "1"
	host := s.sipPublicHost
	if host == "" {
		host = "127.0.0.1"
	}
	// Recent calls involving this extension (best-effort URI/caller-id match).
	recentCalls, _ := s.store.ListCDRsFilteredForTenant(r.Context(), ext.TenantID, store.CDRFilter{
		Search: ext.Extension,
		Limit:  15,
	})
	// Self-service: tenant users for the owner picker.
	tenantUsers, _ := s.store.ListUsersForTenant(r.Context(), ext.TenantID)
	s.renderLayout(w, r, "Ext "+ext.Extension, "extension", map[string]any{
		"Tenant":         tenant,
		"Extension":      ext,
		"VoicemailBox":   vmBox,
		"VoicemailMsgs":  vmMessages,
		"VMTranscripts":  vmTranscripts,
		"SIPDomain":      domain,
		"SIPServerHost":  host,
		"SIPServerPort":  s.sipPublicPort,
		"SIPTransport":   s.sipPublicTransport,
		"RevealPassword": reveal,
		"SIPPassword":    password,
		"RecentCalls":    recentCalls,
		"TenantUsers":    tenantUsers,
	})
}

// extensionSetOwner assigns (or clears) the extension's owning user — the link
// that lets that user manage this extension from the self-service portal.
func (s *Server) extensionSetOwner(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "extensionID"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ext, err := s.lookupExtensionByID(r.Context(), id)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	if !s.canAccessTenant(r.Context(), ext.TenantID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	var ownerID *uuid.UUID
	if v := strings.TrimSpace(r.FormValue("user_id")); v != "" {
		uid, err := uuid.Parse(v)
		if err != nil {
			s.flashErr(w, r, "/admin/extensions/"+id.String(), err)
			return
		}
		// Confirm the chosen user actually belongs to this tenant.
		if _, err := s.store.GetMembership(r.Context(), uid, ext.TenantID); err != nil {
			http.Error(w, "user is not a member of this workspace", http.StatusBadRequest)
			return
		}
		ownerID = &uid
	}
	if err := s.store.SetExtensionUser(r.Context(), id, ownerID); err != nil {
		s.flashErr(w, r, "/admin/extensions/"+id.String(), err)
		return
	}
	s.auditNested(r, ext.TenantID, "extension.owner.set", "extension", &id, map[string]any{
		"user_id": ownerID,
	})
	http.Redirect(w, r, "/admin/extensions/"+id.String()+"?flash=Owner+updated.", http.StatusSeeOther)
}

// extensionRotatePassword mints a new SIP password + HA1, audits, redirects
// back with ?reveal=1 so the user sees the new value once.
func (s *Server) extensionRotatePassword(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "extensionID"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ext, err := s.lookupExtensionByID(r.Context(), id)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	if !s.canAccessTenant(r.Context(), ext.TenantID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if _, err := s.store.RotateExtensionSIPPassword(r.Context(), id); err != nil {
		s.flashErr(w, r, "/admin/extensions/"+id.String(), err)
		return
	}
	ip, ua := audit.FromRequest(r)
	var actorTok *uuid.UUID
	if tok := tokenFromCtx(r.Context()); tok != nil {
		actorTok = &tok.ID
	}
	s.audit.Log(r.Context(), audit.Event{
		TenantID: &ext.TenantID, ActorTokenID: actorTok,
		Event: "extension.password.rotated", TargetType: "extension", TargetID: &id,
		IPAddress: ip, UserAgent: ua,
	})
	http.Redirect(w, r, "/admin/extensions/"+id.String()+"?reveal=1&flash=New+password+below.+Update+your+phone+now.", http.StatusSeeOther)
}

func (s *Server) extensionFeaturesUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "extensionID"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ext, err := s.lookupExtensionByID(r.Context(), id)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	if !s.canAccessTenant(r.Context(), ext.TenantID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	_ = r.ParseForm()
	dnd := r.FormValue("do_not_disturb") == "true"
	vm := r.FormValue("voicemail_enabled") == "true"
	rec := r.FormValue("recording_enabled") == "true"
	cfImm := r.FormValue("cf_immediate")
	cfBusy := r.FormValue("cf_busy")
	cfNA := r.FormValue("cf_no_answer")
	if _, err := s.store.UpdateExtensionFeatures(r.Context(), id, store.UpdateExtensionFeaturesInput{
		DoNotDisturb:     &dnd,
		VoicemailEnabled: &vm,
		RecordingEnabled: &rec,
		CFImmediate:      &cfImm,
		CFBusy:           &cfBusy,
		CFNoAnswer:       &cfNA,
	}); err != nil {
		s.flashErr(w, r, "/admin/extensions/"+id.String(), err)
		return
	}
	http.Redirect(w, r, "/admin/extensions/"+id.String()+"?flash=saved", http.StatusSeeOther)
}

// extensionRename updates the display name. The extension number / SIP identity
// are intentionally immutable (changing them would break registration).
func (s *Server) extensionRename(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "extensionID"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ext, err := s.lookupExtensionByID(r.Context(), id)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	if !s.canAccessTenant(r.Context(), ext.TenantID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	_ = r.ParseForm()
	redirect := "/admin/extensions/" + id.String()
	if err := s.store.UpdateExtensionDisplayName(r.Context(), id, strings.TrimSpace(r.FormValue("display_name"))); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, ext.TenantID, "extension.renamed", "extension", &id, nil)
	http.Redirect(w, r, redirect+"?flash=Saved.", http.StatusSeeOther)
}

// extensionDelete hard-deletes an extension, but only after confirming no DID
// routes to it (those aren't FK-protected and would silently orphan).
func (s *Server) extensionDelete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "extensionID"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ext, err := s.lookupExtensionByID(r.Context(), id)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	if !s.canAccessTenant(r.Context(), ext.TenantID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	redirect := "/admin/extensions/" + id.String()
	dids, err := s.store.ExtensionInboundDIDs(r.Context(), id)
	if err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	if len(dids) > 0 {
		s.flashErr(w, r, redirect, errors.New("reassign the DID(s) routing here first: "+strings.Join(dids, ", ")))
		return
	}
	if err := s.store.DeleteExtension(r.Context(), id); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, ext.TenantID, "extension.deleted", "extension", &id, nil)
	http.Redirect(w, r, "/admin/tenants/"+ext.TenantID.String()+"?flash=Extension+deleted.", http.StatusSeeOther)
}

func (s *Server) extensionVoicemailCreate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "extensionID"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ext, err := s.lookupExtensionByID(r.Context(), id)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	if !s.canAccessTenant(r.Context(), ext.TenantID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	_ = r.ParseForm()
	if _, err := s.store.CreateVoicemailBox(r.Context(), store.CreateVoicemailBoxInput{
		TenantID:    ext.TenantID,
		ExtensionID: id,
		PIN:         r.FormValue("pin"),
		Email:       r.FormValue("email"),
	}); err != nil {
		s.flashErr(w, r, "/admin/extensions/"+id.String(), err)
		return
	}
	http.Redirect(w, r, "/admin/extensions/"+id.String()+"?flash=voicemail+created", http.StatusSeeOther)
}

func (s *Server) lookupExtensionByID(ctx context.Context, id uuid.UUID) (*store.Extension, error) {
	const q = `
		SELECT e.id, e.tenant_id, e.sip_domain_id, e.extension, e.sip_username,
		       '', e.user_id, COALESCE(e.display_name,''),
		       e.voicemail_enabled,
		       e.do_not_disturb, COALESCE(e.cf_immediate,''), COALESCE(e.cf_busy,''),
		       COALESCE(e.cf_no_answer,''), e.recording_enabled,
		       e.status, e.created_at, e.updated_at
		  FROM extensions e WHERE e.id = $1`
	var e store.Extension
	err := s.store.DB.QueryRow(ctx, q, id).Scan(
		&e.ID, &e.TenantID, &e.SIPDomainID, &e.Extension, &e.SIPUsername,
		&e.SIPPassword, &e.UserID, &e.DisplayName,
		&e.VoicemailEnabled,
		&e.DoNotDisturb, &e.CFImmediate, &e.CFBusy, &e.CFNoAnswer, &e.RecordingEnabled,
		&e.Status, &e.CreatedAt, &e.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ---------------------------------------------------------------------------
// API tokens
// ---------------------------------------------------------------------------

func (s *Server) apiTokensList(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.store.ListAPITokens(r.Context())
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	tenants, _ := s.store.ListTenants(r.Context())
	s.renderLayout(w, r, "API tokens", "api_tokens", map[string]any{
		"Tokens":   tokens,
		"Tenants":  tenants,
		"NewToken": r.URL.Query().Get("new"),
	})
}

func (s *Server) apiTokensCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	in := store.CreateAPITokenInput{
		Name:  r.FormValue("name"),
		Scope: r.FormValue("scope"),
	}
	if v := r.FormValue("tenant_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			in.TenantID = &id
		}
	}
	if v := r.FormValue("expires_in"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			t := time.Now().Add(d)
			in.ExpiresAt = &t
		}
	}
	issued, err := s.store.CreateAPIToken(r.Context(), in)
	if err != nil {
		s.flashErr(w, r, "/admin/api-tokens", err)
		return
	}
	http.Redirect(w, r, "/admin/api-tokens?new="+issued.Plaintext, http.StatusSeeOther)
}

func (s *Server) apiTokensRevoke(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "tokenID"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	_ = s.store.RevokeAPIToken(r.Context(), id)
	http.Redirect(w, r, "/admin/api-tokens", http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Render helpers
// ---------------------------------------------------------------------------

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpls.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("portal render", "template", name, "err", err)
	}
}

func (s *Server) renderLayout(w http.ResponseWriter, r *http.Request, title, contentTmpl string, data map[string]any) {
	data["Title"] = title
	// Each content template defines its own uniquely-named block, e.g.
	// "dashboard_content". The layout template invokes {{template .ContentName .}}.
	data["ContentName"] = contentTmpl + "_content"

	tok := tokenFromCtx(r.Context())
	data["User"] = tok

	// Phase 4.4: thread memberships + current-tenant into the layout so
	// the nav can render a workspace switcher.
	// Phase 4.9: also thread the SessionUser (resolved from the portal:
	// token name) so the layout can render the verify-email banner.
	if tok != nil {
		if user, _ := s.userFromSessionToken(r.Context(), tok); user != nil {
			if memberships, err := s.store.ListMembershipsForUser(r.Context(), user.ID); err == nil {
				data["Memberships"] = memberships
			}
			data["SessionUser"] = user
			data["EmailUnverified"] = user.EmailVerifiedAt == nil && user.Role != "super_admin"
		}
		if tok.TenantID != nil {
			data["CurrentTenantID"] = *tok.TenantID
		}
	}

	if f := r.URL.Query().Get("flash"); f != "" {
		data["Flash"] = f
	}
	if f := r.URL.Query().Get("err"); f != "" {
		data["Flash"] = f
		data["FlashErr"] = true
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpls.ExecuteTemplate(w, "layout", data); err != nil {
		slog.Error("portal render layout", "content", contentTmpl, "err", err)
	}
}

func (s *Server) errPage(w http.ResponseWriter, _ *http.Request, err error) {
	slog.Error("portal", "err", err)
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func (s *Server) flashErr(w http.ResponseWriter, r *http.Request, back string, err error) {
	sep := "?"
	if strings.Contains(back, "?") {
		sep = "&"
	}
	http.Redirect(w, r, back+sep+"err="+template.URLQueryEscaper(err.Error()), http.StatusSeeOther)
}

func (s *Server) parseTenantParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	tid, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		http.Error(w, "bad tenant id", 400)
		return uuid.Nil, false
	}
	if !s.canAccessTenant(r.Context(), tid) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return uuid.Nil, false
	}
	return tid, true
}

func (s *Server) canAccessTenant(ctx context.Context, tid uuid.UUID) bool {
	tok := tokenFromCtx(ctx)
	if tok == nil || tok.TenantID == nil {
		return true // super-admin
	}
	return *tok.TenantID == tid
}

// Funcs available in templates. `dyntemplate` lets the layout pick the
// content block by variable name (Go's `template` action requires a literal).
var funcs template.FuncMap

func init() {
	funcs = template.FuncMap{
		"deref": func(s any) string {
			if p, ok := s.(*string); ok && p != nil {
				return *p
			}
			if str, ok := s.(string); ok {
				return str
			}
			return ""
		},
		// dyntemplate is rebound per-Server in New() to close over s.tmpls.
		// This placeholder lets the templates parse.
		"dyntemplate": func(name string, data any) (template.HTML, error) {
			return "", nil
		},
		"humandur": humandur,
	}
}

// humandur formats a duration in seconds as "1h02m", "3m05s", or "12s".
// Accepts int or *int (nil/zero → "—").
func humandur(v any) string {
	var s int
	switch n := v.(type) {
	case int:
		s = n
	case *int:
		if n == nil {
			return "—"
		}
		s = *n
	default:
		return "—"
	}
	if s <= 0 {
		return "—"
	}
	h, m, sec := s/3600, (s%3600)/60, s%60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh%02dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm%02ds", m, sec)
	default:
		return fmt.Sprintf("%ds", sec)
	}
}

// insightFor looks up the AI call insight for a CDR id in the map passed to the
// CDR template. Returns nil when absent (AI off / not yet processed) so the
// template can `{{with insightFor .Insights .ID}}` and render nothing extra.
func insightFor(m map[uuid.UUID]store.CallInsight, id uuid.UUID) *store.CallInsight {
	if m == nil {
		return nil
	}
	if ci, ok := m[id]; ok {
		return &ci
	}
	return nil
}

// ---------------------------------------------------------------------------
// Per-entity list helpers — kept tiny since the store package doesn't yet
// have generic "list extensions/devices for tenant" methods.
// ---------------------------------------------------------------------------

func mustExtensions(ctx context.Context, st *store.Store, tid uuid.UUID) []store.Extension {
	exts, _ := st.ListExtensionsForTenant(ctx, tid)
	return exts
}

func mustDevices(ctx context.Context, st *store.Store, tid uuid.UUID) []store.Device {
	d, _ := st.ListDevicesForTenant(ctx, tid)
	return d
}

func mustSIPDomains(ctx context.Context, st *store.Store, tid uuid.UUID) []store.SIPDomain {
	const q = `SELECT id, tenant_id, domain, is_primary, created_at
	            FROM sip_domains WHERE tenant_id = $1 ORDER BY is_primary DESC, domain`
	rows, err := st.DB.Query(ctx, q, tid)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []store.SIPDomain
	for rows.Next() {
		var d store.SIPDomain
		if err := rows.Scan(&d.ID, &d.TenantID, &d.Domain, &d.IsPrimary, &d.CreatedAt); err == nil {
			out = append(out, d)
		}
	}
	return out
}

func listIVRs(ctx context.Context, st *store.Store, tid uuid.UUID) ([]store.IVR, error) {
	const q = `
		SELECT id, tenant_id, name, COALESCE(extension,''),
		       greeting_long, greeting_short, invalid_sound, exit_sound,
		       timeout_ms, inter_digit_timeout_ms,
		       max_failures, max_timeouts, digit_len,
		       enabled, created_at, updated_at
		  FROM ivrs WHERE tenant_id = $1 AND enabled = true ORDER BY extension NULLS LAST`
	rows, err := st.DB.Query(ctx, q, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.IVR
	for rows.Next() {
		var v store.IVR
		if err := rows.Scan(
			&v.ID, &v.TenantID, &v.Name, &v.Extension,
			&v.GreetingLong, &v.GreetingShort, &v.InvalidSound, &v.ExitSound,
			&v.TimeoutMS, &v.InterDigitTimeoutMS,
			&v.MaxFailures, &v.MaxTimeouts, &v.DigitLen,
			&v.Enabled, &v.CreatedAt, &v.UpdatedAt,
		); err == nil {
			out = append(out, v)
		}
	}
	return out, nil
}

func listQueues(ctx context.Context, st *store.Store, tid uuid.UUID) ([]store.Queue, error) {
	return st.ListQueuesForTenant(ctx, tid)
}
