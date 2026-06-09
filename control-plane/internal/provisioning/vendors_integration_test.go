//go:build integration

package provisioning

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

func provStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping provisioning integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return &store.Store{DB: pool}
}

// seedProvDevice creates a tenant/domain/extension + a device of the given
// vendor with the extension on line 1. Returns the bare MAC, the SIP domain,
// and the username to look for in the rendered config.
func seedProvDevice(t *testing.T, s *store.Store, vendor, mac, number string) (string, string) {
	t.Helper()
	ctx := context.Background()
	ten, err := s.CreateTenant(ctx, "pv-"+uuid.NewString()[:8], "PV IT")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	t.Cleanup(func() { _, _ = s.DB.Exec(context.Background(), "DELETE FROM tenants WHERE id=$1", ten.ID) })
	domain := "pv-" + uuid.NewString()[:8] + ".sip.local"
	sd, err := s.CreateSIPDomain(ctx, ten.ID, domain, true)
	if err != nil {
		t.Fatalf("CreateSIPDomain: %v", err)
	}
	ext, err := s.CreateExtension(ctx, ten.ID, sd.ID, number, number, "pw", "Desk")
	if err != nil {
		t.Fatalf("CreateExtension: %v", err)
	}
	if _, err := s.CreateDevice(ctx, ten.ID, mac, vendor, "model", vendor+" phone"); err != nil {
		t.Fatalf("CreateDevice(%s): %v", vendor, err)
	}
	if _, err := s.CreateDeviceLine(ctx, mac, 1, ext.ID, ""); err != nil {
		t.Fatalf("CreateDeviceLine: %v", err)
	}
	return domain, number
}

func provGet(t *testing.T, s *store.Store, path string) (int, string) {
	t.Helper()
	srv, err := New(s, Config{PublicHost: "prov.example.com", SIPProxy: "sip.example.com", SIPPort: 5060, Transport: "udp"})
	if err != nil {
		t.Fatalf("provisioning.New: %v", err)
	}
	req := httptest.NewRequest("GET", path, nil)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestProvisioningRendersPolycomConfig(t *testing.T) {
	s := provStore(t)
	mac := "0004f2aabbcc"
	domain, user := seedProvDevice(t, s, "polycom", mac, "7601")

	// Polycom phones fetch /<MAC>.cfg, same URL form as Yealink (handler picks
	// the template by the device's declared vendor).
	code, body := provGet(t, s, "/"+mac+".cfg")
	if code != http.StatusOK {
		t.Fatalf("GET /%s.cfg = %d\n%s", mac, code, body)
	}
	for _, want := range []string{user + "@" + domain, `auth.userId="` + user + `"`} {
		if !strings.Contains(body, want) {
			t.Errorf("polycom config missing %q\n%.500s", want, body)
		}
	}
}

func TestProvisioningRendersGrandstreamConfig(t *testing.T) {
	s := provStore(t)
	mac := "000b82aabbcc"
	domain, user := seedProvDevice(t, s, "grandstream", mac, "7701")

	// Grandstream phones fetch /cfg<MAC>.xml.
	code, body := provGet(t, s, "/cfg"+mac+".xml")
	if code != http.StatusOK {
		t.Fatalf("GET /cfg%s.xml = %d\n%s", mac, code, body)
	}
	for _, want := range []string{"<P35>" + user + "</P35>", "<P47>" + domain + "</P47>"} {
		if !strings.Contains(body, want) {
			t.Errorf("grandstream config missing %q\n%.500s", want, body)
		}
	}
}
