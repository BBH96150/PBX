// SAML SP support (sibling of the OIDC code).
//
// Design notes:
//   - Single platform-wide SP keypair loaded from env (SAML_SP_CERT_PEM,
//     SAML_SP_KEY_PEM). All tenants share one SP cert; admins register that
//     cert with their IdP. Much simpler key management than per-tenant.
//   - We construct a fresh saml.ServiceProvider per request from the
//     tenant's stored IdP metadata. Cheap (XML parse) and avoids cross-
//     tenant cache invalidation problems.
//   - HTTP-Redirect binding for AuthnRequest; HTTP-POST binding for ACS.
//   - No assertion encryption, no SLO. Both can be layered on later.
//   - Attribute mapping is configurable per-tenant (attr_email / attr_name);
//     NameID is used as a fallback when attr_email isn't found.

package sso

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
)

// SAMLKeypair is the platform-wide SP keypair, loaded once at startup from env.
type SAMLKeypair struct {
	Cert *x509.Certificate
	Key  *rsa.PrivateKey
}

// NewSAMLKeypairFromEnv loads SAML_SP_CERT_PEM + SAML_SP_KEY_PEM. Returns
// (nil, nil) when unset so the rest of the platform can come up without
// SAML — callers should check and refuse SAML routes when nil.
func NewSAMLKeypairFromEnv() (*SAMLKeypair, error) {
	certPEM := os.Getenv("SAML_SP_CERT_PEM")
	keyPEM := os.Getenv("SAML_SP_KEY_PEM")
	if certPEM == "" || keyPEM == "" {
		return nil, nil
	}
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return nil, errors.New("SAML_SP_CERT_PEM: not a PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("SAML_SP_CERT_PEM: %w", err)
	}
	block, _ = pem.Decode([]byte(keyPEM))
	if block == nil {
		return nil, errors.New("SAML_SP_KEY_PEM: not a PEM block")
	}
	key, err := parseRSAKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("SAML_SP_KEY_PEM: %w", err)
	}
	return &SAMLKeypair{Cert: cert, Key: key}, nil
}

func parseRSAKey(der []byte) (*rsa.PrivateKey, error) {
	if k, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return k, nil
	}
	if k, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		if rk, ok := k.(*rsa.PrivateKey); ok {
			return rk, nil
		}
		return nil, errors.New("PKCS8 key is not RSA")
	}
	return nil, errors.New("not a recognized RSA key format")
}

// SAMLSPInput is everything we need to build a per-request ServiceProvider.
type SAMLSPInput struct {
	Keypair        *SAMLKeypair
	IDPMetadataXML []byte
	EntityID       string   // SP entityID — usually derived from portal URL
	AcsURL         *url.URL // ours: e.g. https://portal/admin/sso/saml/callback
	MetadataURL    *url.URL // ours: e.g. https://portal/admin/sso/saml/metadata
}

// NewSAMLSP constructs a crewjam saml.ServiceProvider parameterized by a
// tenant's IdP metadata + our shared keypair.
func NewSAMLSP(in SAMLSPInput) (*saml.ServiceProvider, error) {
	if in.Keypair == nil {
		return nil, errors.New("SAML keypair not configured (set SAML_SP_CERT_PEM + SAML_SP_KEY_PEM)")
	}
	idpMD, err := samlsp.ParseMetadata(in.IDPMetadataXML)
	if err != nil {
		return nil, fmt.Errorf("parse IdP metadata: %w", err)
	}
	sp := &saml.ServiceProvider{
		EntityID:        in.EntityID,
		Key:             in.Keypair.Key,
		Certificate:     in.Keypair.Cert,
		MetadataURL:     *in.MetadataURL,
		AcsURL:          *in.AcsURL,
		IDPMetadata:     idpMD,
		AuthnNameIDFormat: saml.UnspecifiedNameIDFormat,
		// Reasonable defaults; admins can re-render metadata if they need to
		// pin a particular signature algorithm or NameID format.
	}
	return sp, nil
}

// AuthnRedirectURL builds a HTTP-Redirect AuthnRequest URL.
func AuthnRedirectURL(sp *saml.ServiceProvider, relayState string) (string, error) {
	req, err := sp.MakeAuthenticationRequest(
		sp.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding,
		saml.HTTPPostBinding,
	)
	if err != nil {
		return "", err
	}
	u, err := req.Redirect(relayState, sp)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// ParseACSResponse validates a HTTP-POST ACS response and returns the
// asserted attributes. requestIDs is the set of in-flight AuthnRequest IDs
// (we issue one per login attempt and stash it in the relay-state cookie).
func ParseACSResponse(sp *saml.ServiceProvider, r *http.Request, requestIDs []string) (*saml.Assertion, error) {
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	resp, err := sp.ParseResponse(r, requestIDs)
	if err != nil {
		// crewjam wraps the real error in InvalidResponseError; surface the
		// underlying for our logs.
		var inv *saml.InvalidResponseError
		if errors.As(err, &inv) {
			return nil, fmt.Errorf("SAML response invalid: %w", inv.PrivateErr)
		}
		return nil, err
	}
	return resp, nil
}

// ExtractAttribute pulls a named attribute value out of an assertion, or
// falls back to NameID if the attribute isn't present.
func ExtractAttribute(a *saml.Assertion, name string, fallbackToNameID bool) string {
	for _, stmt := range a.AttributeStatements {
		for _, attr := range stmt.Attributes {
			if attr.Name == name || attr.FriendlyName == name {
				for _, v := range attr.Values {
					if v.Value != "" {
						return v.Value
					}
				}
			}
		}
	}
	if fallbackToNameID && a.Subject != nil && a.Subject.NameID != nil {
		return a.Subject.NameID.Value
	}
	return ""
}

// GenerateSelfSignedSAMLKeypair is a one-off helper for ops: returns
// (certPEM, keyPEM) suitable for SAML_SP_CERT_PEM + SAML_SP_KEY_PEM.
// Use it from a small `cmd/gen-saml-keys` tool or test setup.
func GenerateSelfSignedSAMLKeypair(commonName string, validity time.Duration) (certPEM, keyPEM []byte, err error) {
	return generateSelfSignedRSA(commonName, validity)
}
