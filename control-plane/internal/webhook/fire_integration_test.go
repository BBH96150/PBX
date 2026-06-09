//go:build integration

// Integration test for the full async webhook pipeline: Fire() looks up the
// tenant's enabled endpoints for an event, signs + delivers the payload, and
// records the delivery status. Uses an injected (non-guarded) client so the
// SSRF guard (unit-tested elsewhere) doesn't block the loopback test server.
// Run via `go test -tags=integration ./...` with DATABASE_URL set.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

func TestFireDeliversSignsAndRecords(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping webhook integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	st := &store.Store{DB: pool}
	ctx := context.Background()

	ten, err := st.CreateTenant(ctx, "wh-"+uuid.NewString()[:8], "WH IT")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM tenants WHERE id=$1", ten.ID) })

	type received struct {
		sig  string
		body []byte
	}
	got := make(chan received, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		select {
		case got <- received{sig: r.Header.Get("X-Webhook-Signature"), body: b}:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	secret := "topsecret"
	ep, err := st.CreateWebhookEndpoint(ctx, ten.ID, srv.URL, secret, []string{"call.completed"})
	if err != nil {
		t.Fatalf("CreateWebhookEndpoint: %v", err)
	}

	d := &Dispatcher{store: st, client: srv.Client()} // injected client, no SSRF guard
	d.Fire(ten.ID, "call.completed", map[string]any{"call_uuid": "abc", "duration_sec": 42})

	// The delivery should arrive, signed correctly.
	select {
	case rec := <-got:
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(rec.body)
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if rec.sig != want {
			t.Errorf("signature mismatch:\n got  %s\n want %s", rec.sig, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("webhook was not delivered within 5s")
	}

	// And the delivery status should be recorded as ok.
	deadline := time.Now().Add(3 * time.Second)
	for {
		cur, err := st.GetWebhookEndpointForTenant(ctx, ten.ID, ep.ID)
		if err == nil && cur.LastStatus != nil && *cur.LastStatus == "ok" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("delivery status not recorded as ok in time (last: %+v)", func() any {
				if cur != nil && cur.LastStatus != nil {
					return *cur.LastStatus
				}
				return nil
			}())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestFireSkipsUnsubscribedEvent(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	st := &store.Store{DB: pool}
	ctx := context.Background()

	ten, err := st.CreateTenant(ctx, "wh-"+uuid.NewString()[:8], "WH IT2")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM tenants WHERE id=$1", ten.ID) })

	hit := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case hit <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Subscribed only to call.completed.
	if _, err := st.CreateWebhookEndpoint(ctx, ten.ID, srv.URL, "s", []string{"call.completed"}); err != nil {
		t.Fatalf("CreateWebhookEndpoint: %v", err)
	}
	d := &Dispatcher{store: st, client: srv.Client()}
	d.Fire(ten.ID, "trunk.down", map[string]any{"trunk": "x"}) // not subscribed

	select {
	case <-hit:
		t.Fatal("endpoint received an event it was not subscribed to")
	case <-time.After(800 * time.Millisecond):
		// good — no delivery
	}
}
