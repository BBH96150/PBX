module github.com/tendpos/sip-platform/control-plane

go 1.25.0

// Pin the build toolchain to 1.25.11, which patches two stdlib CVEs flagged by
// govulncheck: GO-2026-5039 (net/textproto) and GO-2026-5037 (crypto/x509).
toolchain go1.25.11

require (
	github.com/coreos/go-oidc/v3 v3.18.0
	github.com/crewjam/saml v0.5.1
	github.com/getkin/kin-openapi v0.140.0
	github.com/go-chi/chi/v5 v5.1.0
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.7.1
	github.com/percipia/eslgo v1.5.1
	github.com/pquerna/otp v1.5.0
	github.com/redis/go-redis/v9 v9.20.0
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
	github.com/yuin/goldmark v1.8.2
	golang.org/x/crypto v0.52.0
	golang.org/x/oauth2 v0.36.0
)

require (
	github.com/beevik/etree v1.6.0 // indirect
	github.com/boombuler/barcode v1.0.1-0.20190219062509-6c824513bacc // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/go-openapi/jsonpointer v0.22.5 // indirect
	github.com/go-openapi/swag/jsonname v0.25.5 // indirect
	github.com/golang-jwt/jwt/v4 v4.5.2 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jonboulle/clockwork v0.5.0 // indirect
	github.com/mattermost/xml-roundtrip-validator v0.1.0 // indirect
	github.com/oasdiff/yaml v0.1.0 // indirect
	github.com/oasdiff/yaml3 v0.0.13 // indirect
	github.com/russellhaering/goxmldsig v1.6.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/text v0.37.0 // indirect
)
