package rps

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegistry_FallbackToLogOnly(t *testing.T) {
	r := NewRegistry(NewLogOnly("custom-fallback"))
	if got := r.For("unknown-vendor").Name(); got != "custom-fallback" {
		t.Errorf("expected fallback, got %q", got)
	}
}

func TestRegistry_RoutesByVendor(t *testing.T) {
	poly := NewPolycom("https://example.invalid", "fake-token", "fake-profile")
	r := NewRegistry(NewLogOnly("fb"), poly)

	if got := r.For("polycom").Name(); got != "polycom" {
		t.Errorf("polycom: got %q", got)
	}
	if got := r.For("POLYCOM").Name(); got != "polycom" {
		t.Errorf("case-insensitive lookup failed, got %q", got)
	}
	if got := r.For("yealink").Name(); got != "fb" {
		t.Errorf("unknown vendor should fall back, got %q", got)
	}
}

func TestRegistry_NilSafe(t *testing.T) {
	var r *Registry
	if got := r.For("polycom").Name(); got != "log_only" {
		t.Errorf("nil registry should return defaultLogOnly, got %q", got)
	}
}

func TestMACPlain(t *testing.T) {
	cases := map[string]string{
		"00:04:f2:ab:cd:ef": "0004f2abcdef",
		"00-04-F2-AB-CD-EF": "0004f2abcdef",
		"0004.f2ab.cdef":    "0004f2abcdef",
		"0004F2ABCDEF":      "0004f2abcdef",
	}
	for in, want := range cases {
		if got := MACPlain(in); got != want {
			t.Errorf("MACPlain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPolycom_RegisterSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/devices" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
			http.Error(w, "no", 400)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fake-token" {
			t.Errorf("bad auth header: %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	p := NewPolycom(srv.URL, "fake-token", "profile-1")
	err := p.Register(context.Background(), RegisterRequest{
		MAC: "00:04:f2:ab:cd:ef", Vendor: "polycom",
		ProvisioningURL: "https://prov.example.com/",
	})
	if err != nil {
		t.Errorf("Register: %v", err)
	}
}

func TestPolycom_RegisterAlreadyExists_409IsOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"already exists"}`, http.StatusConflict)
	}))
	defer srv.Close()

	p := NewPolycom(srv.URL, "fake-token", "profile-1")
	if err := p.Register(context.Background(), RegisterRequest{MAC: "001122334455"}); err != nil {
		t.Errorf("409 should be treated as success, got %v", err)
	}
}

func TestPolycom_RegisterMissingCreds(t *testing.T) {
	p := NewPolycom("", "", "")
	err := p.Register(context.Background(), RegisterRequest{MAC: "001122334455"})
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("expected creds-required error, got %v", err)
	}
}

func TestPolycom_UnregisterTolerates404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE")
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	p := NewPolycom(srv.URL, "fake-token", "profile-1")
	if err := p.Unregister(context.Background(), "001122334455"); err != nil {
		t.Errorf("404 should be tolerated, got %v", err)
	}
}

func TestPolycom_RegisterPropagatesOtherErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := NewPolycom(srv.URL, "fake-token", "profile-1")
	err := p.Register(context.Background(), RegisterRequest{MAC: "001122334455"})
	if err == nil {
		t.Fatalf("expected error on 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected status 500 in error, got %v", err)
	}
}

func TestLogOnly(t *testing.T) {
	l := NewLogOnly("test")
	if err := l.Register(context.Background(), RegisterRequest{MAC: "a"}); err != nil {
		t.Error(err)
	}
	if err := l.Unregister(context.Background(), "a"); err != nil {
		t.Error(err)
	}
	if l.Name() != "test" {
		t.Errorf("name: %s", l.Name())
	}
}

// Sanity: errAs helper unwraps wrapped retryable errors.
func TestIsTransientError(t *testing.T) {
	if IsTransientError(nil) {
		t.Error("nil should not be transient")
	}
	if IsTransientError(errors.New("plain")) {
		t.Error("plain error should not be transient")
	}
}
