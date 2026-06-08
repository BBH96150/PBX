package api

import (
	"bytes"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// The embedded OpenAPI spec (served at /v1/docs and /v1/openapi.yaml) must
// always parse and validate — this guards against accidental corruption or an
// invalid hand-edit slipping into the served docs.
func TestEmbeddedOpenAPISpecIsValid(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapiSpec)
	if err != nil {
		t.Fatalf("openapi: load failed: %v", err)
	}
	if err := doc.Validate(loader.Context); err != nil {
		t.Fatalf("openapi: spec invalid: %v", err)
	}
	if doc.Info == nil || doc.Info.Title == "" {
		t.Error("openapi: missing info.title")
	}
	// Sanity: the spec actually documents the core surface.
	for _, want := range []string{"/v1/tenants", "paging-groups", "X-Webhook-Signature", "bearerAuth"} {
		if !bytes.Contains(openapiSpec, []byte(want)) {
			t.Errorf("openapi: spec missing expected content %q", want)
		}
	}
}
