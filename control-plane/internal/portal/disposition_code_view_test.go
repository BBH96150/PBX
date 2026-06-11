package portal

import (
	"html/template"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// newTestTmpls parses the embedded template tree the same way New() does, so the
// template-parse tests guard against syntax errors and missing-field panics.
func newTestTmpls(t *testing.T) (*Server, *template.Template) {
	t.Helper()
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
	return srv, tmpl
}

// TestDispositionCodesTemplateParseAndRender renders the disposition-codes
// management page through the layout, both with codes and empty.
func TestDispositionCodesTemplateParseAndRender(t *testing.T) {
	srv, _ := newTestTmpls(t)
	tenant := &store.Tenant{ID: uuid.New(), Name: "Acme"}

	codes := []store.DispositionCode{
		{ID: uuid.New(), TenantID: tenant.ID, Label: "Sale", Color: "#22aa55", SortOrder: 0, Active: true},
		{ID: uuid.New(), TenantID: tenant.ID, Label: "Spam", SortOrder: 1, Active: false},
	}
	data := map[string]any{
		"Title":       "test",
		"ContentName": "disposition_codes_content",
		"Tenant":      tenant,
		"NavActive":   "dispositions",
		"Codes":       codes,
	}
	if err := srv.tmpls.ExecuteTemplate(httptest.NewRecorder(), "layout", data); err != nil {
		t.Fatalf("render disposition_codes: %v", err)
	}

	empty := map[string]any{
		"Title": "test", "ContentName": "disposition_codes_content",
		"Tenant": tenant, "NavActive": "dispositions",
		"Codes": []store.DispositionCode{},
	}
	if err := srv.tmpls.ExecuteTemplate(httptest.NewRecorder(), "layout", empty); err != nil {
		t.Fatalf("render empty disposition_codes: %v", err)
	}
}

// TestCDRsTemplateWithDispositions renders the CDR list with the per-row
// disposition <select> populated, guarding the assignment-dropdown block.
func TestCDRsTemplateWithDispositions(t *testing.T) {
	srv, _ := newTestTmpls(t)
	tenant := &store.Tenant{ID: uuid.New(), Name: "Acme"}

	codeID := uuid.New()
	codes := []store.DispositionCode{
		{ID: codeID, TenantID: tenant.ID, Label: "Sale", Color: "#22aa55", Active: true},
		{ID: uuid.New(), TenantID: tenant.ID, Label: "Support", Active: true},
	}
	cdrs := []store.CDR{
		{
			ID: uuid.New(), Direction: "inbound", FromURI: "sip:a@x", ToURI: "sip:1001@x",
			CallerIDNum: "+15551112222", HangupCause: "NORMAL_CLEARING",
			DispositionCodeID: &codeID, DispositionLabel: "Sale", DispositionColor: "#22aa55",
		},
		{
			ID: uuid.New(), Direction: "outbound", FromURI: "sip:1001@x", ToURI: "sip:b@x",
			CallerIDNum: "1001", HangupCause: "NORMAL_CLEARING",
		},
	}
	data := map[string]any{
		"Title": "test", "ContentName": "cdrs_content",
		"Tenant": tenant, "NavActive": "cdrs",
		"CDRs": cdrs, "Insights": map[uuid.UUID]store.CallInsight{},
		"Dispositions": codes,
		"Direction":    "", "Search": "", "Since": "", "Until": "",
	}
	if err := srv.tmpls.ExecuteTemplate(httptest.NewRecorder(), "layout", data); err != nil {
		t.Fatalf("render cdrs with dispositions: %v", err)
	}
}
