//go:build integration

// HTTP-level integration tests for the admin portal: real router + store +
// Postgres + a real portal session cookie. Excluded from the default build;
// run via `go test -tags=integration ./...` with DATABASE_URL set.
package portal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tendpos/sip-platform/control-plane/internal/audit"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

func testPortal(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping portal integration test")
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
	srv, err := New(st, Options{Audit: audit.New(pool), PortalBaseURL: "https://pbx.test"})
	if err != nil {
		t.Fatalf("portal.New: %v", err)
	}
	return srv, st
}

// seedPortalSession creates a tenant + admin user + a portal-format session
// token, returning the tenant id and the cookie value.
func seedPortalSession(t *testing.T, st *store.Store) (uuid.UUID, string) {
	t.Helper()
	ctx := context.Background()
	ten, err := st.CreateTenant(ctx, "pt-"+uuid.NewString()[:8], "Portal IT")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	t.Cleanup(func() { _, _ = st.DB.Exec(context.Background(), "DELETE FROM tenants WHERE id=$1", ten.ID) })

	email := "admin-" + uuid.NewString()[:8] + "@example.com"
	if _, err := st.CreateUser(ctx, store.CreateUserInput{
		TenantID: &ten.ID, Email: email, DisplayName: "Admin", Role: "tenant_admin", Password: "pw-secret-123",
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// Session tokens carry the special "portal:<email>:<ts>" name the portal
	// uses to resolve the logged-in user.
	tok, err := st.CreateAPIToken(ctx, store.CreateAPITokenInput{
		TenantID: &ten.ID, Name: "portal:" + email + ":20260101T000000", Scope: "admin",
	})
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	return ten.ID, tok.Plaintext
}

func getAuthed(t *testing.T, h http.Handler, path, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: cookie})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPortalAuthedPagesRender(t *testing.T) {
	srv, st := testPortal(t)
	h := srv.Router()
	tid, cookie := seedPortalSession(t, st)

	paths := []string{
		"/help",
		"/help/paging-groups",
		"/broadcast",
		"/tenants/" + tid.String(),
		"/tenants/" + tid.String() + "/paging",
		"/tenants/" + tid.String() + "/cdrs",
	}
	for _, p := range paths {
		rec := getAuthed(t, h, p, cookie)
		if rec.Code != http.StatusOK {
			body := rec.Body.String()
			if len(body) > 300 {
				body = body[:300]
			}
			t.Errorf("GET %s = %d, want 200\n%s", p, rec.Code, body)
		}
	}

	// The Help Center must render the rewritten KB links (markdown was processed).
	rec := getAuthed(t, h, "/help", cookie)
	if !strings.Contains(rec.Body.String(), "/admin/help/") {
		t.Error("help index should contain rewritten KB links")
	}
}

func TestPortalRequiresAuth(t *testing.T) {
	srv, st := testPortal(t)
	h := srv.Router()
	tid, _ := seedPortalSession(t, st)

	req := httptest.NewRequest("GET", "/tenants/"+tid.String(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
		t.Errorf("no-cookie request = %d, want a redirect to login", rec.Code)
	}
}
