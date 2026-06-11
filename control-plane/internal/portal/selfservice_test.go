package portal

import (
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// TestUserOwnsExtension covers the self-service ownership guard predicate:
// the owner passes; a same-tenant non-owner is rejected; a cross-tenant user
// is rejected; an unowned extension is rejected. Tenant match alone is never
// sufficient — that is the authorization gap this feature closes.
func TestUserOwnsExtension(t *testing.T) {
	tenantA := uuid.New()
	tenantB := uuid.New()
	owner := &store.User{ID: uuid.New(), TenantID: &tenantA}
	sameTenantOther := &store.User{ID: uuid.New(), TenantID: &tenantA}
	crossTenant := &store.User{ID: uuid.New(), TenantID: &tenantB}

	ownerID := owner.ID
	ext := &store.Extension{ID: uuid.New(), TenantID: tenantA, UserID: &ownerID}
	unowned := &store.Extension{ID: uuid.New(), TenantID: tenantA, UserID: nil}

	cases := []struct {
		name string
		ext  *store.Extension
		u    *store.User
		want bool
	}{
		{"owner passes", ext, owner, true},
		{"same-tenant non-owner rejected", ext, sameTenantOther, false},
		{"cross-tenant rejected", ext, crossTenant, false},
		{"unowned extension rejected", unowned, owner, false},
		{"nil user rejected", ext, nil, false},
		{"nil extension rejected", nil, owner, false},
	}
	for _, c := range cases {
		if got := userOwnsExtension(c.ext, c.u); got != c.want {
			t.Errorf("%s: userOwnsExtension = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestSelfServiceTemplatesParseAndRender builds the embedded template tree the
// same way New() does and renders the layout for both self-service content
// blocks, guarding against template syntax errors and missing-field panics.
func TestSelfServiceTemplatesParseAndRender(t *testing.T) {
	srv := &Server{}
	tmpl := template.New("").Funcs(template.FuncMap{
		"deref":       funcs["deref"],
		"dyntemplate": srv.dyntemplate,
		"humandur":    humandur,
		"insightFor":  insightFor,
	})
	if _, err := tmpl.ParseFS(tmplFS, "templates/*.html"); err != nil {
		t.Fatalf("ParseFS: %v", err)
	}
	srv.tmpls = tmpl

	ownerID := uuid.New()
	ext := &store.Extension{
		ID: uuid.New(), TenantID: uuid.New(), Extension: "1001",
		DisplayName: "Front Desk", UserID: &ownerID, VoicemailEnabled: true,
	}

	render := func(content string, data map[string]any) {
		data["Title"] = "test"
		data["ContentName"] = content + "_content"
		if err := srv.tmpls.ExecuteTemplate(httptest.NewRecorder(), "layout", data); err != nil {
			t.Errorf("render %s: %v", content, err)
		}
	}

	render("me", map[string]any{
		"SelfService": true,
		"Extensions":  []*store.Extension{ext},
	})
	render("me_extension", map[string]any{
		"SelfService":   true,
		"Extension":     ext,
		"VoicemailBox":  nil,
		"VoicemailMsgs": []store.VoicemailMessage{},
		"VMTranscripts": map[uuid.UUID]string{},
		"RecentCalls":   nil,
	})
}

func TestAdminScopeRequired(t *testing.T) {
	s := &Server{}
	const passed = 299
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(passed) })
	h := s.adminScopeRequired(next)

	call := func(scope, path string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if scope != "" {
			req = req.WithContext(context.WithValue(req.Context(), ctxKeyToken, &store.APIToken{Scope: scope}))
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr.Code
	}

	cases := []struct {
		scope, path string
		want        int
	}{
		{"admin", "/", passed},
		{"admin", "/tenants/abc", passed},
		{"admin", "/api-tokens", passed},
		{"write", "/me", passed},
		{"write", "/me/extensions/abc", passed},
		{"read", "/security/2fa", passed},
		{"read", "/softphone", passed},
		{"write", "/switch-tenant", passed},
		{"write", "/", http.StatusSeeOther},
		{"write", "/tenants/abc", http.StatusSeeOther},
		{"read", "/api-tokens", http.StatusSeeOther},
		{"read", "/ops/live", http.StatusSeeOther},
		{"write", "/menu", http.StatusSeeOther}, // /me prefix must not match /menu
	}
	for _, c := range cases {
		if got := call(c.scope, c.path); got != c.want {
			t.Errorf("scope=%q path=%q: got %d, want %d", c.scope, c.path, got, c.want)
		}
	}
}
