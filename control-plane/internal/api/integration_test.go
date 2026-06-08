//go:build integration

// HTTP-level integration tests for the /v1 API: real chi router + real store +
// real Postgres + real bearer-token auth. Excluded from the default build;
// run via `go test -tags=integration ./...` with DATABASE_URL set (CI
// `integration` job). Validates the wire contract documented in openapi.yaml.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

func testServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping API integration test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	t.Cleanup(pool.Close)
	st := &store.Store{DB: pool}
	return New(st, Options{}), st
}

// seedTenantWithAdminToken returns a tenant id + an admin bearer token scoped
// to it, cleaning up the tenant (cascade) at test end.
func seedTenantWithAdminToken(t *testing.T, st *store.Store) (string, string) {
	t.Helper()
	ctx := context.Background()
	ten, err := st.CreateTenant(ctx, "api-"+randSuffix(), "API IT")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	t.Cleanup(func() { _, _ = st.DB.Exec(context.Background(), "DELETE FROM tenants WHERE id=$1", ten.ID) })
	tok, err := st.CreateAPIToken(ctx, store.CreateAPITokenInput{TenantID: &ten.ID, Name: "it", Scope: "admin"})
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	return ten.ID.String(), tok.Plaintext
}

func randSuffix() string {
	return uuid.NewString()[:8]
}

func do(t *testing.T, h http.Handler, method, path, token string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestAPIPublicAndAuth(t *testing.T) {
	srv, _ := testServer(t)
	h := srv.Router()

	// Public: version + healthz + docs need no token.
	for _, p := range []string{"/healthz", "/v1/version", "/v1/openapi.yaml", "/v1/docs"} {
		rec, _ := do(t, h, "GET", p, "", nil)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s public = %d, want 200", p, rec.Code)
		}
	}

	// Protected endpoint without a token → 401.
	rec, _ := do(t, h, "GET", "/v1/tenants", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /v1/tenants no-token = %d, want 401", rec.Code)
	}
}

func TestAPITenantAndContactFlow(t *testing.T) {
	srv, st := testServer(t)
	h := srv.Router()
	tid, token := seedTenantWithAdminToken(t, st)

	// GET the tenant with a valid token.
	rec, _ := do(t, h, "GET", "/v1/tenants/"+tid, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET tenant = %d, want 200", rec.Code)
	}

	// Create a contact (write scope; admin token has it).
	rec, _ = do(t, h, "POST", "/v1/tenants/"+tid+"/contacts", token, map[string]any{
		"name": "Grace Hopper", "number": "+14155550123",
	})
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("POST contact = %d (%s)", rec.Code, rec.Body.String())
	}

	// List contacts → our contact is present.
	rec, _ = do(t, h, "GET", "/v1/tenants/"+tid+"/contacts", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET contacts = %d", rec.Code)
	}
	var contacts []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &contacts); err != nil {
		t.Fatalf("contacts not a JSON array: %v", err)
	}
	if len(contacts) != 1 {
		t.Fatalf("want 1 contact, got %d", len(contacts))
	}

	// Paging-groups endpoint responds (empty list ok).
	rec, _ = do(t, h, "GET", "/v1/tenants/"+tid+"/paging-groups", token, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("GET paging-groups = %d, want 200", rec.Code)
	}
}
