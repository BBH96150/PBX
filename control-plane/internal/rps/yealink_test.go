package rps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestYealink_RegisterSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/open/v1/rps/device" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth: %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	y := NewYealink(srv.URL, "tok", "my-server")
	if err := y.Register(context.Background(), RegisterRequest{MAC: "00:15:65:aa:bb:cc"}); err != nil {
		t.Errorf("Register: %v", err)
	}
}

func TestYealink_MissingCreds(t *testing.T) {
	y := NewYealink("", "", "")
	if err := y.Register(context.Background(), RegisterRequest{MAC: "001565aabbcc"}); err == nil {
		t.Error("expected creds-required error")
	}
}

func TestGrandstream_RegisterSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1.0.0/device/data/add" {
			t.Errorf("path: %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	g := NewGrandstream(srv.URL, "tok", "org-1")
	if err := g.Register(context.Background(), RegisterRequest{MAC: "C0:74:AD:01:23:45", Model: "GRP2614"}); err != nil {
		t.Errorf("Register: %v", err)
	}
}

func TestGrandstream_UnregisterTolerates404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()
	g := NewGrandstream(srv.URL, "tok", "org-1")
	if err := g.Unregister(context.Background(), "C074AD012345"); err != nil {
		t.Errorf("404 should be tolerated, got %v", err)
	}
}

// Registry routes to the right adapter when multiple are registered.
func TestRegistry_MultipleVendors(t *testing.T) {
	r := NewRegistry(
		NewLogOnly("fb"),
		NewPolycom("https://x", "t", "p"),
		NewYealink("https://y", "t", "s"),
		NewGrandstream("https://z", "t", "o"),
	)
	if r.For("polycom").Name() != "polycom" {
		t.Error("polycom route")
	}
	if r.For("yealink").Name() != "yealink" {
		t.Error("yealink route")
	}
	if r.For("grandstream").Name() != "grandstream" {
		t.Error("grandstream route")
	}
	if r.For("snom").Name() != "fb" {
		t.Error("snom should fall back")
	}
}
