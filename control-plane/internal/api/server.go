package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// QueueProvisioner is the minimal interface the admin API needs to push
// queue/agent changes live to mod_callcenter (Wave 4.5). Implemented by
// internal/freeswitch.ESLClient.
//
// A nil Provisioner is fine — the API persists to DB and skips ESL sync;
// admin can always fall back to `reload mod_callcenter`.
type QueueProvisioner interface {
	SyncQueueToFS(ctx context.Context, queueID uuid.UUID) error
	SyncAgentForExtension(ctx context.Context, extensionID uuid.UUID) error
}

// RPSProvisioner pushes a new device's MAC to its vendor's redirection
// service (Task #10). nil → skip silently.
type RPSProvisioner interface {
	Register(ctx context.Context, vendor, model, mac, provisioningURL, tenantSlug string) error
	Unregister(ctx context.Context, vendor, mac string) error
}

// Mailer is the SMTP sender the auth flows need (invites + reset + verify).
// Real impl: internal/smtp.Mailer. nil → emails skipped.
type Mailer interface {
	SendInvite(to, inviterName, tenantName, acceptURL string) error
	SendPasswordReset(to, resetURL string) error
	SendEmailVerification(to, verifyURL string) error
}

type Server struct {
	store    *store.Store
	queueESL QueueProvisioner
	rps      RPSProvisioner
	mailer   Mailer

	// provisioningPublicHost is handed to RPS providers so the vendor knows
	// where to redirect phones. Set via Options.
	provisioningPublicHost string
	// portalBaseURL is the public URL the portal is reachable at — used
	// to build the accept-invite / password-reset links we email.
	portalBaseURL string
}

type Options struct {
	QueueESL               QueueProvisioner
	RPS                    RPSProvisioner
	Mailer                 Mailer
	ProvisioningPublicHost string
	PortalBaseURL          string
}

func New(s *store.Store, opts Options) *Server {
	return &Server{
		store:                  s,
		queueESL:               opts.QueueESL,
		rps:                    opts.RPS,
		mailer:                 opts.Mailer,
		provisioningPublicHost: opts.ProvisioningPublicHost,
		portalBaseURL:          opts.PortalBaseURL,
	}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(loggerMiddleware)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(15 * time.Second))

	// Phase 4.0 auth — Bearer token required everywhere except the
	// whitelisted paths (health checks + FS xml_curl callbacks which FS
	// can't authenticate against).
	r.Use(s.AuthMiddleware([]string{
		"/healthz",
		"/v1/version",
		"/v1/freeswitch/dialplan",
		"/v1/freeswitch/directory",
		"/v1/freeswitch/configuration",
		"/v1/signup",         // Phase 4.4: public self-serve signup
		"/v1/invites/accept", // Phase 4.5: invite acceptance is public
		"/v1/password-reset", // Phase 4.5: forgot-password endpoints are public
	}))

	r.Get("/healthz", healthz)
	r.Get("/v1/version", version)

	r.Route("/v1/api-tokens", func(r chi.Router) {
		r.Post("/", RequireScope("admin", s.createAPIToken))
		r.Get("/", RequireScope("admin", s.listAPITokens))
		r.Delete("/{tokenID}", RequireScope("admin", s.revokeAPIToken))
	})

	r.Route("/v1/users", func(r chi.Router) {
		r.Post("/", RequireScope("admin", s.createUser))
		r.Post("/{userID}/password", RequireScope("admin", s.setUserPassword))
	})

	r.Post("/v1/signup", s.signup) // public; whitelisted above

	// Phase 4.5: invites + password reset.
	r.Route("/v1/tenants/{tenantID}/invites", func(r chi.Router) {
		r.Post("/", RequireScope("admin", s.createInvite))
		r.Get("/", RequireScope("admin", s.listInvites))
	})
	r.Delete("/v1/invites/{inviteID}", RequireScope("admin", s.revokeInvite))
	r.Post("/v1/invites/accept", s.acceptInvite)                 // public
	r.Post("/v1/password-reset/request", s.requestPasswordReset) // public
	r.Post("/v1/password-reset/confirm", s.confirmPasswordReset) // public

	r.Route("/v1/tenants", func(r chi.Router) {
		r.Post("/", s.createTenant)
		r.Get("/", s.listTenants)
		r.Route("/{tenantID}", func(r chi.Router) {
			r.Get("/", s.getTenant)
			r.Post("/sip-domains", s.createSIPDomain)
			r.Post("/extensions", s.createExtension)
			r.Get("/extensions", s.listExtensions)
			r.Get("/cdrs", s.listCDRs)
			r.Get("/contacts", s.listContacts)
			r.Post("/contacts", RequireScope("write", s.createContact))
			r.Delete("/contacts/{contactID}", RequireScope("write", s.deleteContact))
			r.Post("/devices", s.createDevice)
			r.Post("/dids", s.createDID)
			r.Get("/dids", s.listDIDs)
			r.Post("/ring-groups", s.createRingGroup)
			r.Get("/ring-groups", s.listRingGroups)
			r.Get("/queues", s.listQueues)
			r.Get("/paging-groups", s.listPagingGroups)
			r.Get("/devices", s.listDevices)
		})
	})

	r.Get("/v1/devices/{mac}", s.getDevice)
	r.Post("/v1/devices/{mac}/lines", s.createDeviceLine)

	r.Get("/v1/carriers", s.listCarriers)
	r.Post("/v1/carriers/{kind}/accounts", s.createCarrierAccount)

	r.Post("/v1/ring-groups/{ringGroupID}/members", s.addRingGroupMember)

	r.Route("/v1/extensions/{extensionID}/voicemail", func(r chi.Router) {
		r.Post("/", s.createVoicemailBox)
		r.Get("/", s.getVoicemailBox)
		r.Get("/messages", s.listVoicemailMessages)
	})
	r.Patch("/v1/extensions/{extensionID}/features", s.updateExtensionFeatures)

	r.Post("/v1/tenants/{tenantID}/ivrs", s.createIVR)
	r.Post("/v1/ivrs/{ivrID}/options", s.addIVROption)

	r.Post("/v1/tenants/{tenantID}/queues", s.createQueue)
	r.Post("/v1/queues/{queueID}/agents", s.addQueueAgent)

	return r
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func version(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"service": "control-plane", "phase": 1})
}

func loggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		slog.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"dur_ms", time.Since(t).Milliseconds(),
			"remote", r.RemoteAddr,
		)
	})
}
