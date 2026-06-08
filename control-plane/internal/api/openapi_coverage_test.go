package api

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
)

// normalizePath collapses {param} placeholders so we compare path STRUCTURE,
// not the (sometimes differently-named) parameter labels between chi and the
// spec. e.g. "/v1/tenants/{tenantID}/cdrs" → "/v1/tenants/{}/cdrs".
var paramRe = regexp.MustCompile(`\{[^}]+\}`)

func normalizePath(p string) string {
	p = paramRe.ReplaceAllString(p, "{}")
	return strings.TrimSuffix(p, "/")
}

// routesNotInSpec are intentionally undocumented in the OpenAPI spec: health
// checks, the docs endpoints themselves, and the FreeSWITCH xml_curl callbacks
// (FS-internal, not a public API).
var routesNotInSpec = map[string]bool{
	"/healthz":                     true,
	"/v1/openapi.yaml":             true,
	"/v1/docs":                     true,
	"/v1/freeswitch/dialplan":      true,
	"/v1/freeswitch/directory":     true,
	"/v1/freeswitch/configuration": true,
}

// TestOpenAPICoversEveryRoute walks the real chi router and asserts that every
// mounted /v1 route is documented in the served OpenAPI spec. This catches the
// common drift where an endpoint is added but the docs aren't updated.
func TestOpenAPICoversEveryRoute(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapiSpec)
	if err != nil {
		t.Fatalf("openapi load: %v", err)
	}
	specPaths := map[string]bool{}
	for p := range doc.Paths.Map() {
		specPaths[normalizePath(p)] = true
	}

	srv := New(nil, Options{}) // nil store is fine — Router() only registers handlers
	router, ok := srv.Router().(chi.Routes)
	if !ok {
		t.Fatal("router is not chi.Routes")
	}

	var missing []string
	checked := 0
	seen := map[string]bool{}
	err = chi.Walk(router, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/v1/") {
			return nil
		}
		norm := normalizePath(route)
		if routesNotInSpec[strings.TrimSuffix(route, "/")] || routesNotInSpec[norm] {
			return nil
		}
		if seen[norm+method] {
			return nil
		}
		seen[norm+method] = true
		checked++
		if !specPaths[norm] {
			missing = append(missing, method+" "+route+" (normalized "+norm+")")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	// Guard against a vacuous pass (e.g. router introspection silently failing).
	if checked < 20 {
		t.Fatalf("only walked %d /v1 routes — router introspection looks broken", checked)
	}
	if len(missing) > 0 {
		t.Errorf("OpenAPI spec is missing %d documented route(s):\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}
