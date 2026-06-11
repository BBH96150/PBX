package portal

import (
	"html/template"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

func TestSummarizeReadiness(t *testing.T) {
	checks := []ReadinessCheck{
		{Name: "a", Status: "ok"},
		{Name: "b", Status: "ok"},
		{Name: "c", Status: "warn"},
		{Name: "d", Status: "fail"},
		{Name: "e", Status: "fail"},
		{Name: "f", Status: "weird"}, // unknown status is ignored
	}
	got := summarizeReadiness(checks)
	if got.OK != 2 || got.Warn != 1 || got.Fail != 2 {
		t.Errorf("summarizeReadiness = %+v, want {OK:2 Warn:1 Fail:2}", got)
	}
	// Empty input must not panic and must zero out.
	if z := summarizeReadiness(nil); z.OK != 0 || z.Warn != 0 || z.Fail != 0 {
		t.Errorf("summarizeReadiness(nil) = %+v, want all zero", z)
	}
}

func TestOverallStatus(t *testing.T) {
	cases := []struct {
		s    readinessSummary
		want string
	}{
		{readinessSummary{OK: 3}, "ok"},
		{readinessSummary{OK: 2, Warn: 1}, "warn"},
		{readinessSummary{OK: 2, Warn: 1, Fail: 1}, "fail"}, // fail dominates warn
		{readinessSummary{}, "ok"},                          // no checks → ok
	}
	for _, c := range cases {
		if got := overallStatus(c.s); got != c.want {
			t.Errorf("overallStatus(%+v) = %q, want %q", c.s, got, c.want)
		}
	}
}

// TestReadinessTemplateParseAndRender parses the embedded template tree the same
// way New() does and renders the readiness content block through the layout,
// guarding against template syntax errors and missing-field panics.
func TestReadinessTemplateParseAndRender(t *testing.T) {
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

	tenant := &store.Tenant{ID: uuid.New(), Name: "Acme"}
	checks := []ReadinessCheck{
		{Name: "Carrier trunks", Status: "ok", Detail: "2 trunks configured and registered."},
		{Name: "Inbound numbers", Status: "warn", Detail: "1 number has no destination.", FixHref: "/admin/tenants/x/dids"},
		{Name: "E911 locations", Status: "fail", Detail: "No extension has a location.", FixHref: "/admin/tenants/x/e911"},
	}
	data := map[string]any{
		"Title":       "test",
		"ContentName": "readiness_content",
		"Tenant":      tenant,
		"NavActive":   "readiness",
		"Checks":      checks,
		"Summary":     summarizeReadiness(checks),
		"Overall":     overallStatus(summarizeReadiness(checks)),
	}
	if err := srv.tmpls.ExecuteTemplate(httptest.NewRecorder(), "layout", data); err != nil {
		t.Fatalf("render readiness: %v", err)
	}

	// All-clear render (no checks needing a fix) must also be panic-free.
	allOK := []ReadinessCheck{{Name: "Carrier trunks", Status: "ok", Detail: "ok"}}
	empty := map[string]any{
		"Title": "test", "ContentName": "readiness_content",
		"Tenant": tenant, "NavActive": "readiness",
		"Checks": allOK, "Summary": summarizeReadiness(allOK), "Overall": "ok",
	}
	if err := srv.tmpls.ExecuteTemplate(httptest.NewRecorder(), "layout", empty); err != nil {
		t.Fatalf("render all-ok readiness: %v", err)
	}
}
