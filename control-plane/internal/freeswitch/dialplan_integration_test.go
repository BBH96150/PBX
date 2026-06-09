//go:build integration

// End-to-end test for the xml_curl dialplan handler — the control-plane acting
// as FreeSWITCH's routing brain. Builds a real request like FS sends on each
// call and asserts the returned dialplan XML. Run via `go test -tags=integration`.
package freeswitch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

func dpStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping dialplan integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return &store.Store{DB: pool}
}

func dpSeed(t *testing.T, s *store.Store) (tenantID uuid.UUID, domain string, ext *store.Extension) {
	t.Helper()
	ctx := context.Background()
	ten, err := s.CreateTenant(ctx, "dp-"+uuid.NewString()[:8], "DP IT")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	t.Cleanup(func() { _, _ = s.DB.Exec(context.Background(), "DELETE FROM tenants WHERE id=$1", ten.ID) })
	domain = "dp-" + uuid.NewString()[:8] + ".sip.local"
	sd, err := s.CreateSIPDomain(ctx, ten.ID, domain, true)
	if err != nil {
		t.Fatalf("CreateSIPDomain: %v", err)
	}
	ext, err = s.CreateExtension(ctx, ten.ID, sd.ID, "6501", "6501", "pw", "Desk")
	if err != nil {
		t.Fatalf("CreateExtension: %v", err)
	}
	return ten.ID, domain, ext
}

// dialplanXML drives the handler with a FreeSWITCH-style request.
func dialplanXML(t *testing.T, h *Handler, dest, domain string) (int, string) {
	t.Helper()
	form := url.Values{
		"section":                            {"dialplan"},
		"destination_number":                 {dest},
		"context":                            {"default"},
		"variable_sip_h_X-Sip-Tenant-Domain": {domain},
		"Caller-Caller-ID-Number":            {"1000"},
	}
	req := httptest.NewRequest("POST", "/v1/freeswitch/dialplan", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestDialplanRoutesToExtension(t *testing.T) {
	s := dpStore(t)
	_, domain, _ := dpSeed(t, s)
	h := NewHandler(s, "kam.example:5060")

	code, xml := dialplanXML(t, h, "6501", domain)
	if code != http.StatusOK {
		t.Fatalf("dialplan status = %d", code)
	}
	for _, want := range []string{
		`application="bridge"`,
		"sofia/internal/sip:6501@" + domain,
		"fs_path=sip:kam.example:5060",
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("extension dialplan missing %q\n%s", want, xml)
		}
	}
}

func TestDialplanRoutesPageCodeToConference(t *testing.T) {
	s := dpStore(t)
	ctx := context.Background()
	tid, domain, ext := dpSeed(t, s)

	pg, err := s.CreatePagingGroup(ctx, store.CreatePagingGroupInput{
		TenantID: tid, Name: "All", Extension: "800", Mode: "fs_conference",
	})
	if err != nil {
		t.Fatalf("CreatePagingGroup: %v", err)
	}
	if _, err := s.AddPagingMember(ctx, pg.ID, ext.ID); err != nil {
		t.Fatalf("AddPagingMember: %v", err)
	}

	h := NewHandler(s, "kam.example:5060")
	code, xml := dialplanXML(t, h, "800", domain)
	if code != http.StatusOK {
		t.Fatalf("paging dialplan status = %d", code)
	}
	for _, want := range []string{
		`application="conference"`,
		"@paging",
		"conference_set_auto_outcall",
		"sofia/internal/sip:6501@" + domain, // the member is auto-outcalled
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("paging dialplan missing %q\n%s", want, xml)
		}
	}
}
