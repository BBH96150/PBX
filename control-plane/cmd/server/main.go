package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/tendpos/sip-platform/control-plane/internal/api"
	"github.com/tendpos/sip-platform/control-plane/internal/audit"
	"github.com/tendpos/sip-platform/control-plane/internal/config"
	"github.com/tendpos/sip-platform/control-plane/internal/crypto"
	"github.com/tendpos/sip-platform/control-plane/internal/freeswitch"
	"github.com/tendpos/sip-platform/control-plane/internal/portal"
	"github.com/tendpos/sip-platform/control-plane/internal/provisioning"
	"github.com/tendpos/sip-platform/control-plane/internal/rps"
	"github.com/tendpos/sip-platform/control-plane/internal/smtp"
	"github.com/tendpos/sip-platform/control-plane/internal/sso"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// rpsAdapter bridges *rps.Registry to api.RPSProvisioner so the api package
// doesn't need to import internal/rps directly.
type rpsAdapter struct{ registry *rps.Registry }

func (a rpsAdapter) Register(ctx context.Context, vendor, model, mac, provURL, tenantSlug string) error {
	return a.registry.Register(ctx, rps.RegisterRequest{
		Vendor: vendor, Model: model, MAC: mac,
		ProvisioningURL: provURL, TenantSlug: tenantSlug,
	})
}
func (a rpsAdapter) Unregister(ctx context.Context, vendor, mac string) error {
	return a.registry.Unregister(ctx, vendor, mac)
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.FromEnv()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL, cfg.RedisURL)
	if err != nil {
		slog.Error("store open", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	// Phase 4.0: bootstrap admin token if env is set and table is empty.
	if cfg.BootstrapAPIToken != "" {
		n, err := st.CountAPITokens(ctx)
		if err != nil {
			slog.Error("count api tokens", "err", err)
		} else if n == 0 {
			t, err := st.BootstrapAPIToken(ctx, cfg.BootstrapAPIToken, "bootstrap")
			if err != nil {
				slog.Error("bootstrap api token", "err", err)
			} else {
				slog.Info("bootstrap admin token inserted", "id", t.ID, "prefix", t.TokenPrefix)
			}
		}
	}

	// Phase 4.3: bootstrap super-admin portal user if env is set and users table is empty.
	if cfg.BootstrapUserEmail != "" && cfg.BootstrapUserPassword != "" {
		n, err := st.CountUsers(ctx)
		if err != nil {
			slog.Error("count users", "err", err)
		} else if n == 0 {
			u, err := st.BootstrapUser(ctx, cfg.BootstrapUserEmail, "Bootstrap Admin", cfg.BootstrapUserPassword)
			if err != nil {
				slog.Error("bootstrap user", "err", err)
			} else {
				slog.Info("bootstrap super-admin user inserted", "email", u.Email, "id", u.ID)
			}
		}
	}

	// Admin API + FreeSWITCH mod_xml_curl handler share the admin listener
	// (FreeSWITCH talks to us on http://control-plane:8080/v1/freeswitch/dialplan).
	mailer := smtp.Mailer{
		Host:        cfg.SMTPHost,
		Port:        cfg.SMTPPort,
		Username:    cfg.SMTPUsername,
		Password:    cfg.SMTPPassword,
		From:        cfg.SMTPFrom,
		UseSTARTTLS: cfg.SMTPUseSTARTTLS,
	}
	esl := freeswitch.NewESLClient(cfg.ESLHost, cfg.ESLPort, cfg.ESLPassword, st, mailer, cfg.KamailioSIPTarget)

	// Task #10: RPS registry. Real Polycom adapter if creds are set;
	// otherwise everything falls back to LogOnly.
	var rpsAdapters []rps.Provider
	if cfg.PolycomZTPAPIToken != "" && cfg.PolycomZTPProfileID != "" {
		rpsAdapters = append(rpsAdapters,
			rps.NewPolycom(cfg.PolycomZTPAPIBase, cfg.PolycomZTPAPIToken, cfg.PolycomZTPProfileID))
		slog.Info("rps: polycom ZTP enabled")
	}
	rpsRegistry := rps.NewRegistry(rps.NewLogOnly("fallback"), rpsAdapters...)

	adminMux := http.NewServeMux()
	adminAPI := api.New(st, api.Options{
		QueueESL:               esl,
		RPS:                    rpsAdapter{registry: rpsRegistry},
		Mailer:                 mailer, // smtp.Mailer satisfies api.Mailer
		ProvisioningPublicHost: cfg.ProvisioningHost,
		PortalBaseURL:          cfg.PortalBaseURL,
	})

	// Phase 4.6: audit logger writes to audit_log via the same DB pool.
	auditLog := audit.New(st.DB)

	// Phase 4.7: AES-GCM sealer for TOTP secrets. Refuse to start if any
	// users are already enrolled and the key isn't set — losing the key
	// after enrollment would silently brick those accounts.
	sealer, err := crypto.NewSealerFromEnv()
	if err != nil {
		slog.Error("TOTP sealer init", "err", err)
		os.Exit(1)
	}
	if sealer == nil {
		if enrolled, _ := st.HasAnyEnrolledTwoFA(context.Background()); enrolled {
			slog.Error("TOTP_ENCRYPTION_KEY is not set but users have enrolled 2FA — refusing to start")
			os.Exit(1)
		}
		slog.Warn("TOTP_ENCRYPTION_KEY not set; 2FA enrollment will be disabled")
	}

	// Phase 4.8: OIDC SSO manager (caches discovered providers per issuer).
	ssoMgr := sso.New()

	// Phase 4.10: SAML SP keypair (platform-wide). nil → SAML routes refuse.
	samlKey, err := sso.NewSAMLKeypairFromEnv()
	if err != nil {
		slog.Error("SAML keypair init", "err", err)
		os.Exit(1)
	}
	if samlKey == nil {
		slog.Warn("SAML_SP_CERT_PEM / SAML_SP_KEY_PEM not set; SAML SSO disabled")
	}

	// Phase 4.1: web admin portal at /admin/* (server-rendered HTML).
	// Phase 5.1: per-tenant carrier gateway provisioner. Writes gateway XML
	// to the shared volume + asks Sofia to rescan via ESL.
	gwProvisionerCore := freeswitch.NewGatewayProvisioner(st, esl, cfg.FSDynamicGatewayDir, cfg.FSLogDir)
	gwProvisioner := gwAdapter{inner: gwProvisionerCore}
	// Initial reconcile at boot: anything in the DB → on-disk → Sofia.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := gwProvisionerCore.SyncGateways(ctx); err != nil {
			slog.Warn("initial gateway sync failed", "err", err)
		}
	}()

	portalSrv, err := portal.New(st, portal.Options{
		Mailer:               mailer, // smtp.Mailer satisfies portal.Mailer
		PortalBaseURL:        cfg.PortalBaseURL,
		Audit:                auditLog,
		Sealer:               sealer,
		SSO:                  ssoMgr,
		SAMLKey:              samlKey,
		GatewaySyncer:        gwProvisioner,
		Originator:           esl, // *ESLClient satisfies portal.CallOriginator via Originate
		SIPPublicHost:        cfg.SIPPublicHost,
		SIPPublicPort:        cfg.SIPPublicPort,
		SIPPublicTransport:   cfg.SIPPublicTransport,
		SIPDomainSuffix:      cfg.SIPDomainSuffix,
		VoicemailStorageRoot: cfg.VoicemailStorageRoot,
		RecordingStorageRoot: cfg.RecordingStorageRoot,
	})
	if err != nil {
		slog.Error("portal init", "err", err)
		os.Exit(1)
	}
	adminMux.Handle("/admin/", http.StripPrefix("/admin", portalSrv.Router()))
	adminMux.Handle("/v1/", adminAPI.Router())
	adminMux.Handle("/v1/freeswitch/dialplan", freeswitch.NewHandler(st, cfg.KamailioSIPTarget))
	adminMux.Handle("/v1/freeswitch/directory", freeswitch.NewDirectoryHandler(st))
	adminMux.Handle("/v1/freeswitch/configuration", freeswitch.NewConfigurationHandler(st, cfg.KamailioSIPTarget))
	adminMux.Handle("/v1/sms/inbound", freeswitch.NewSMSInboundHandler(st))
	adminMux.Handle("/healthz", adminAPI.Router())
	adminMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/admin/", http.StatusSeeOther)
			return
		}
		http.NotFound(w, r)
	})

	provServer, err := provisioning.New(st, provisioning.Config{
		PublicHost: cfg.ProvisioningHost,
		SIPProxy:   cfg.SIPPublicHost,
		SIPPort:    cfg.SIPPublicPort,
		Transport:  cfg.SIPPublicTransport,
	})
	if err != nil {
		slog.Error("provisioning init", "err", err)
		os.Exit(1)
	}

	admin := &http.Server{Addr: cfg.AdminAddr, Handler: adminMux, ReadHeaderTimeout: 5 * time.Second}
	prov := &http.Server{Addr: cfg.ProvisioningAddr, Handler: provServer.Router(), ReadHeaderTimeout: 5 * time.Second}

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		slog.Info("admin api listening", "addr", cfg.AdminAddr)
		if err := admin.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("admin server error", "err", err)
		}
	}()

	go func() {
		defer wg.Done()
		if cfg.ProvisioningCert != "" && cfg.ProvisioningKey != "" {
			slog.Info("provisioning server listening (TLS)", "addr", cfg.ProvisioningAddr)
			if err := prov.ListenAndServeTLS(cfg.ProvisioningCert, cfg.ProvisioningKey); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("provisioning server error", "err", err)
			}
			return
		}
		slog.Warn("provisioning TLS cert/key not set; serving plain HTTP (dev only)", "addr", cfg.ProvisioningAddr)
		if err := prov.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("provisioning server error", "err", err)
		}
	}()

	go func() {
		defer wg.Done()
		esl.Run(ctx)
	}()

	_ = gwProvisioner // keep the variable in scope for the adapter; satisfied here.

	<-ctx.Done()
	slog.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = admin.Shutdown(shutdownCtx)
	_ = prov.Shutdown(shutdownCtx)
	wg.Wait()
	slog.Info("shutdown complete")
}

// gwAdapter bridges freeswitch.GatewayProvisioner → portal.GatewaySyncer.
// The portal package defines its own GatewayLiveStatus to avoid importing
// freeswitch; this adapter does the trivial field copy.
type gwAdapter struct {
	inner *freeswitch.GatewayProvisioner
}

func (a gwAdapter) SyncGateways(ctx context.Context) error {
	return a.inner.SyncGateways(ctx)
}

func (a gwAdapter) GatewayStatus(ctx context.Context, gatewayName string) portal.GatewayLiveStatus {
	s := a.inner.GatewayStatus(ctx, gatewayName)
	return portal.GatewayLiveStatus{
		Found:       s.Found,
		State:       s.State,
		Status:      s.Status,
		PingTime:    s.PingTime,
		Uptime:      s.Uptime,
		CallsIn:     s.CallsIn,
		CallsOut:    s.CallsOut,
		Error:       s.Error,
		LastSIPCode: s.LastSIPCode,
		LastSIPMsg:  s.LastSIPMsg,
	}
}
