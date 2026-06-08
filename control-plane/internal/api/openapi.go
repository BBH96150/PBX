package api

import (
	_ "embed"
	"net/http"
)

// openapiSpec is the canonical OpenAPI 3 description of the /v1 API, served at
// GET /v1/openapi.yaml and rendered by the Swagger UI at GET /v1/docs. Keep it
// in sync with docs/openapi.yaml (same content; this copy is embedded so the
// binary can serve it).
//
//go:embed openapi.yaml
var openapiSpec []byte

func (s *Server) serveOpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(openapiSpec)
}

// serveAPIDocs renders an interactive Swagger UI pointed at the spec above.
// Public (no auth) so integrators can read the docs before they have a token.
func (s *Server) serveAPIDocs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(swaggerUIHTML))
}

const swaggerUIHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>SIP Platform API — Reference</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>body{margin:0} .topbar{display:none}</style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js" crossorigin></script>
  <script>
    window.ui = SwaggerUIBundle({
      url: '/v1/openapi.yaml',
      dom_id: '#swagger-ui',
      deepLinking: true,
      docExpansion: 'list',
      defaultModelsExpandDepth: 1,
    });
  </script>
</body>
</html>`
