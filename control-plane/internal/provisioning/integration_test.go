//go:build integration

// End-to-end test for the ZTP provisioning HTTP server: a device + line in the
// store, fetched at the vendor's boot URL, must render a config carrying the
// extension's real credentials. Exercises store → render context → template →
// HTTP. Excluded from the default build; run via `go test -tags=integration`.
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

func TestProvisioningRendersYealinkConfig(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping provisioning integration test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	st := &store.Store{DB: pool}
	ctx := context.Background()

	// Tenant + domain + extension.
	ten, err := st.CreateTenant(ctx, "prov-"+uuid.NewString()[:8], "Prov IT")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM tenants WHERE id=$1", ten.ID) })

	domain := "prov-" + uuid.NewString()[:8] + ".sip.local"
	sd, err := st.CreateSIPDomain(ctx, ten.ID, domain, true)
	if err != nil {
		t.Fatalf("CreateSIPDomain: %v", err)
	}
	ext, err := st.CreateExtension(ctx, ten.ID, sd.ID, "5501", "5501", "secretpw", "Reception")
	if err != nil {
		t.Fatalf("CreateExtension: %v", err)
	}

	// A Yealink device with the extension on line 1.
	mac := "001565aabbcc"
	if _, err := st.CreateDevice(ctx, ten.ID, mac, "yealink", "t46u", "Reception phone"); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	if _, err := st.CreateDeviceLine(ctx, mac, 1, ext.ID, ""); err != nil {
		t.Fatalf("CreateDeviceLine: %v", err)
	}

	srv, err := New(st, Config{PublicHost: "prov.example.com", SIPProxy: "sip.example.com", SIPPort: 5060, Transport: "udp"})
	if err != nil {
		t.Fatalf("provisioning.New: %v", err)
	}

	// The phone fetches /<MAC>.cfg on boot.
	req := httptest.NewRequest("GET", "/"+mac+".cfg", nil)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /%s.cfg = %d, want 200\n%s", mac, rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"5501",                 // username
		domain,                 // SIP realm/server
		"sip.example.com",      // outbound proxy
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered config missing %q\n--- body (first 600) ---\n%.600s", want, body)
		}
	}
}
