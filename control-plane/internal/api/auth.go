package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// ctxKey is the unexported key type for context values stamped by the
// auth middleware. Handlers read these via the exported accessors below.
type ctxKey int

const (
	ctxKeyToken ctxKey = iota
)

// AuthMiddleware verifies the Bearer token on every request. Whitelisted
// paths (health checks, FS xml_curl callbacks) skip auth — listed at the
// caller's discretion.
//
// On success, stamps the token onto the request context so downstream
// handlers can do tenant-scoping checks.
func (s *Server) AuthMiddleware(skipPrefixes []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, p := range skipPrefixes {
				if r.URL.Path == p || strings.HasPrefix(r.URL.Path, p+"/") {
					next.ServeHTTP(w, r)
					return
				}
			}

			plaintext := extractBearer(r)
			if plaintext == "" {
				writeErr(w, http.StatusUnauthorized, "missing Bearer token")
				return
			}
			tok, err := s.store.VerifyAPIToken(r.Context(), plaintext)
			if err != nil {
				switch {
				case errors.Is(err, store.ErrInvalidToken):
					writeErr(w, http.StatusUnauthorized, "invalid token")
				case errors.Is(err, store.ErrExpiredToken):
					writeErr(w, http.StatusUnauthorized, "token expired")
				default:
					slog.Error("token verify", "err", err)
					writeErr(w, http.StatusInternalServerError, "auth check failed")
				}
				return
			}

			// Tenant scoping: a tenant-scoped token can only hit URLs for
			// its own tenant. Detect tenantID URL params and enforce.
			if tok.TenantID != nil {
				if urlTID, ok := urlTenantID(r.URL.Path); ok {
					if urlTID != *tok.TenantID {
						writeErr(w, http.StatusForbidden, "token scoped to a different tenant")
						return
					}
				}
			}

			// Async best-effort: bump last_used_at. Don't block the request.
			go func(id uuid.UUID) {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = s.store.UpdateAPITokenLastUsed(ctx, id)
			}(tok.ID)

			ctx := context.WithValue(r.Context(), ctxKeyToken, tok)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AuthToken returns the verified token from context, or nil for whitelisted
// (unauthenticated) requests.
func AuthToken(ctx context.Context) *store.APIToken {
	if v, ok := ctx.Value(ctxKeyToken).(*store.APIToken); ok {
		return v
	}
	return nil
}

// RequireScope wraps a handler so it 403s unless the token has the required
// scope (or higher).
func RequireScope(required string, next http.HandlerFunc) http.HandlerFunc {
	rank := func(s string) int {
		switch s {
		case "read":
			return 1
		case "write":
			return 2
		case "admin":
			return 3
		}
		return 0
	}
	want := rank(required)
	return func(w http.ResponseWriter, r *http.Request) {
		tok := AuthToken(r.Context())
		if tok == nil || rank(tok.Scope) < want {
			writeErr(w, http.StatusForbidden, "scope "+required+" required")
			return
		}
		next(w, r)
	}
}

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[len("Bearer "):])
	}
	// Some clients prefer X-API-Key for non-OAuth bearer-style auth.
	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}

// urlTenantID scans a path for /v1/tenants/{uuid}/... and returns the UUID.
// Returns false if no tenant component is present or it's malformed.
func urlTenantID(p string) (uuid.UUID, bool) {
	const prefix = "/v1/tenants/"
	if !strings.HasPrefix(p, prefix) {
		return uuid.Nil, false
	}
	rest := p[len(prefix):]
	end := strings.IndexByte(rest, '/')
	if end == -1 {
		end = len(rest)
	}
	id, err := uuid.Parse(rest[:end])
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}
