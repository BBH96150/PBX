// Package sso wraps coreos/go-oidc + golang.org/x/oauth2 with the bits we
// need for per-tenant OIDC: provider discovery, auth URL building with PKCE,
// token exchange, ID token verification.
//
// One Manager instance is shared by the portal — it caches discovered
// providers by issuer URL so we don't refetch the discovery document on
// every request.
package sso

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Manager struct {
	mu    sync.Mutex
	cache map[string]*cached
}

type cached struct {
	provider *oidc.Provider
	at       time.Time
}

// New constructs a Manager. Providers are lazily discovered + cached for
// 1h to avoid refetching the /.well-known/openid-configuration on every
// auth request.
func New() *Manager { return &Manager{cache: map[string]*cached{}} }

func (m *Manager) provider(ctx context.Context, issuerURL string) (*oidc.Provider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.cache[issuerURL]; ok && time.Since(c.at) < time.Hour {
		return c.provider, nil
	}
	p, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery: %w", err)
	}
	m.cache[issuerURL] = &cached{provider: p, at: time.Now()}
	return p, nil
}

// Config is everything we need to mint an auth URL or exchange a code.
type Config struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string // ours, e.g. https://portal/admin/sso/callback
	Scopes       []string
}

// AuthURL builds the IdP authorization URL with PKCE. Caller persists
// state + nonce + pkceVerifier in a short-lived cookie until the callback.
func (m *Manager) AuthURL(ctx context.Context, cfg Config, state, nonce, pkceVerifier string) (string, error) {
	p, err := m.provider(ctx, cfg.IssuerURL)
	if err != nil {
		return "", err
	}
	oa := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     p.Endpoint(),
		Scopes:       cfg.Scopes,
	}
	pkceChallenge := pkceS256(pkceVerifier)
	return oa.AuthCodeURL(state,
		oauth2.AccessTypeOnline,
		oidc.Nonce(nonce),
		oauth2.SetAuthURLParam("code_challenge", pkceChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	), nil
}

// Tokens is the post-exchange claim bundle we surface up.
type Tokens struct {
	Issuer     string
	Subject    string
	Email      string
	Name       string
	RawClaims  map[string]any
	IDTokenRaw string
}

// Exchange swaps the auth code for tokens, verifies the ID token signature
// and nonce, returns the user-relevant claims.
func (m *Manager) Exchange(ctx context.Context, cfg Config, code, pkceVerifier, expectedNonce string) (*Tokens, error) {
	p, err := m.provider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, err
	}
	oa := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     p.Endpoint(),
		Scopes:       cfg.Scopes,
	}
	tok, err := oa.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", pkceVerifier),
	)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		return nil, errors.New("OIDC response missing id_token")
	}
	verifier := p.Verifier(&oidc.Config{ClientID: cfg.ClientID})
	idTok, err := verifier.Verify(ctx, rawID)
	if err != nil {
		return nil, fmt.Errorf("id token verify: %w", err)
	}
	if expectedNonce != "" && idTok.Nonce != expectedNonce {
		return nil, errors.New("OIDC nonce mismatch")
	}
	var claims map[string]any
	if err := idTok.Claims(&claims); err != nil {
		return nil, fmt.Errorf("id token claims: %w", err)
	}
	out := &Tokens{
		Issuer:     idTok.Issuer,
		Subject:    idTok.Subject,
		RawClaims:  claims,
		IDTokenRaw: rawID,
	}
	if v, ok := claims["email"].(string); ok {
		out.Email = v
	}
	if v, ok := claims["name"].(string); ok {
		out.Name = v
	} else if v, ok := claims["given_name"].(string); ok {
		out.Name = v
	}
	return out, nil
}

// Probe is for the admin "test connection" button — just fetches the
// discovery doc and returns the issuer's metadata.
func (m *Manager) Probe(ctx context.Context, issuerURL string) error {
	_, err := m.provider(ctx, issuerURL)
	return err
}

// ---------------------------------------------------------------------------
// PKCE + state/nonce helpers
// ---------------------------------------------------------------------------

func NewState() string { return random(32) }
func NewNonce() string { return random(32) }

// NewPKCEVerifier returns a random S256 verifier (43+ chars per RFC 7636).
func NewPKCEVerifier() string { return random(64) }

func pkceS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func random(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
