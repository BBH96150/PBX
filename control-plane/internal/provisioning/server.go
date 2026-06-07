package provisioning

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// Config is wired from cmd/server/main.go.
type Config struct {
	PublicHost string
	SIPProxy   string
	SIPPort    int
	Transport  string // udp / tcp / tls
}

type Server struct {
	store    *store.Store
	cfg      Config
	registry *registry
}

func New(s *store.Store, cfg Config) (*Server, error) {
	reg, err := loadTemplates()
	if err != nil {
		return nil, err
	}
	return &Server{store: s, cfg: cfg, registry: reg}, nil
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","service":"provisioning"}`))
	})

	// Vendor URL conventions phones actually request on boot.
	r.Get("/{mac:[0-9A-Fa-f]{12}}.cfg", s.handlePolycomOrYealink)
	r.Get("/cfg{mac:[0-9A-Fa-f]{12}}.xml", s.renderVendor("grandstream", "grandstream/grp.xml", "application/xml"))
	r.Get("/cfg{mac:[0-9A-Fa-f]{12}}", s.renderVendor("grandstream", "grandstream/grp.xml", "application/xml"))

	// Placeholders for the next wave (Cisco / Snom / Fanvil — Task #9 follow-up).
	r.Get("/{mac:[0-9A-Fa-f]{12}}.cnf.xml", s.todo)
	r.Get("/settings.xml", s.todo)

	return r
}

// handlePolycomOrYealink picks the right template based on the device's
// declared vendor, since both vendors collide on the `<MAC>.cfg` URL form.
func (s *Server) handlePolycomOrYealink(w http.ResponseWriter, r *http.Request) {
	mac := chi.URLParam(r, "mac")
	cfg, err := s.store.LookupDeviceConfig(r.Context(), mac)
	if err != nil {
		s.lookupErr(w, mac, err)
		return
	}
	switch strings.ToLower(cfg.Device.Vendor) {
	case "polycom":
		s.render(w, r, cfg, "polycom/master.cfg", "application/xml")
	case "yealink", "fanvil":
		s.render(w, r, cfg, "yealink/account.cfg", "text/plain; charset=utf-8")
	default:
		slog.Warn("vendor mismatch for <MAC>.cfg URL",
			"mac", mac, "vendor", cfg.Device.Vendor)
		http.Error(w, "no template for vendor", http.StatusNotFound)
	}
}

// renderVendor returns a handler bound to a fixed (vendor, template) pair.
func (s *Server) renderVendor(expectVendor, tmpl, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mac := chi.URLParam(r, "mac")
		cfg, err := s.store.LookupDeviceConfig(r.Context(), mac)
		if err != nil {
			s.lookupErr(w, mac, err)
			return
		}
		if !strings.EqualFold(cfg.Device.Vendor, expectVendor) {
			slog.Warn("vendor mismatch",
				"mac", mac, "url_expects", expectVendor, "device_vendor", cfg.Device.Vendor)
			http.Error(w, "vendor mismatch", http.StatusBadRequest)
			return
		}
		s.render(w, r, cfg, tmpl, contentType)
	}
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, cfg *store.DeviceConfig, tmpl, contentType string) {
	if len(cfg.Lines) == 0 {
		http.Error(w, "device has no bound lines", http.StatusConflict)
		return
	}

	rc := buildContext(cfg, SIPCtx{
		Proxy:       s.cfg.SIPProxy,
		ProxyPort:   s.cfg.SIPPort,
		Transport:   s.cfg.Transport,
		RegisterExp: 600,
		Codecs:      []string{"OPUS", "G722", "PCMU", "PCMA"},
	}, ProvisionCtx{
		PublicHost: s.cfg.PublicHost,
		Token:      cfg.Device.Token,
	})

	// PTT increment 3: tell the phone which multicast paging groups to listen
	// on (the ones its bound extensions belong to). Best-effort — a lookup
	// failure must not break provisioning.
	extIDs := make([]uuid.UUID, 0, len(cfg.Lines))
	for _, l := range cfg.Lines {
		extIDs = append(extIDs, l.ExtensionID)
	}
	if groups, err := s.store.ListMulticastPagingForExtensions(r.Context(), extIDs); err != nil {
		slog.Warn("multicast paging lookup failed", "mac", cfg.Device.MAC, "err", err)
	} else {
		rc.Paging = pagingMulticastCtx(groups)
	}

	var buf bytes.Buffer
	if err := s.registry.execute(&buf, tmpl, rc); err != nil {
		slog.Error("template render", "tmpl", tmpl, "err", err)
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Provisioned-For", cfg.Device.MAC)
	_, _ = w.Write(buf.Bytes())

	// Best-effort: record the hit. Don't fail the response on error.
	if err := s.store.RecordProvisioningHit(r.Context(), cfg.Device.MAC, clientIP(r), r.UserAgent()); err != nil {
		slog.Warn("record provisioning hit", "err", err)
	}
	slog.Info("provisioned device",
		"mac", cfg.Device.MAC, "vendor", cfg.Device.Vendor,
		"model", cfg.Device.Model, "tenant", cfg.Tenant.Slug,
		"lines", len(cfg.Lines), "ip", clientIP(r),
	)
}

func (s *Server) lookupErr(w http.ResponseWriter, mac string, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		slog.Info("unknown device requested provisioning", "mac", mac)
		http.Error(w, "unknown device", http.StatusNotFound)
		return
	}
	slog.Error("device lookup failed", "mac", mac, "err", err)
	http.Error(w, "lookup failed", http.StatusInternalServerError)
}

func (s *Server) todo(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "provisioning template not implemented yet", http.StatusNotImplemented)
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i > 0 {
		host = host[:i]
	}
	return host
}
