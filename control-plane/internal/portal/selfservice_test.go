package portal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

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
