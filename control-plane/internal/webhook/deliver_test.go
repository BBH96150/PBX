package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// DeliverOne signs the body with HMAC-SHA256 and sets the documented headers.
// We inject a plain client so the SSRF guard (tested separately) doesn't block
// the loopback test server.
func TestDeliverOneSignsAndPostsPayload(t *testing.T) {
	var gotBody []byte
	var gotSig, gotEvent, gotID, gotCT string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotSig = r.Header.Get("X-Webhook-Signature")
		gotEvent = r.Header.Get("X-Webhook-Event")
		gotID = r.Header.Get("X-Webhook-Id")
		gotCT = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := &Dispatcher{client: ts.Client()}
	id := uuid.New()
	ep := store.WebhookEndpoint{ID: id, URL: ts.URL, Secret: "topsecret"}

	code, err := d.DeliverOne(context.Background(), ep, "call.completed", map[string]any{"k": "v"})
	if err != nil {
		t.Fatalf("DeliverOne: %v", err)
	}
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}

	// Headers.
	if gotEvent != "call.completed" {
		t.Errorf("X-Webhook-Event = %q", gotEvent)
	}
	if gotID != id.String() {
		t.Errorf("X-Webhook-Id = %q want %q", gotID, id.String())
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q", gotCT)
	}

	// Signature must be HMAC-SHA256 of the exact raw body with the secret.
	mac := hmac.New(sha256.New, []byte("topsecret"))
	mac.Write(gotBody)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if gotSig != want {
		t.Errorf("signature mismatch:\n got  %s\n want %s", gotSig, want)
	}

	// Body envelope shape.
	var env map[string]any
	if err := json.Unmarshal(gotBody, &env); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if env["event"] != "call.completed" {
		t.Errorf("envelope event = %v", env["event"])
	}
	if _, ok := env["sent_at"]; !ok {
		t.Error("envelope missing sent_at")
	}
	data, ok := env["data"].(map[string]any)
	if !ok || data["k"] != "v" {
		t.Errorf("envelope data = %v", env["data"])
	}
}

func TestDeliverOneNon2xxReturnsError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	d := &Dispatcher{client: ts.Client()}
	ep := store.WebhookEndpoint{ID: uuid.New(), URL: ts.URL, Secret: "s"}
	code, err := d.DeliverOne(context.Background(), ep, "test.ping", nil)
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", code)
	}
}
