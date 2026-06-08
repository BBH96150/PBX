//go:build integration

// Integration tests exercise real store methods against a live Postgres with
// the full migration set applied. They are excluded from the default build and
// only run under `go test -tags=integration` with DATABASE_URL pointing at a
// migrated database (see the CI `integration` job). This keeps the normal
// `go test ./...` suite DB-free (local Postgres can't run in some dev envs).
package store

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	t.Cleanup(pool.Close)
	return &Store{DB: pool}
}

// makeTenant creates a uniquely-slugged tenant and registers cleanup that
// cascade-deletes it (and everything FK-linked) at test end.
func makeTenant(t *testing.T, s *Store) *Tenant {
	t.Helper()
	ctx := context.Background()
	slug := "it-" + uuid.NewString()[:8]
	ten, err := s.CreateTenant(ctx, slug, "Integration "+slug)
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.DB.Exec(context.Background(), "DELETE FROM tenants WHERE id=$1", ten.id())
	})
	return ten
}

// id() shim: Tenant.ID is the field; helper keeps cleanup readable even if the
// field name shifts.
func (t *Tenant) id() uuid.UUID { return t.ID }

func TestIntegrationTenantCreateGetList(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)

	got, err := s.GetTenant(ctx, ten.ID)
	if err != nil {
		t.Fatalf("GetTenant: %v", err)
	}
	if got.ID != ten.ID || got.Slug != ten.Slug {
		t.Fatalf("GetTenant mismatch: %+v vs %+v", got, ten)
	}
}

func TestIntegrationContactCRUD(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)

	c, err := s.CreateContact(ctx, ten.ID, "Ada Lovelace", "+14155550101", "Analytical", "vip")
	if err != nil {
		t.Fatalf("CreateContact: %v", err)
	}
	list, err := s.ListContactsForTenant(ctx, ten.ID, "")
	if err != nil {
		t.Fatalf("ListContacts: %v", err)
	}
	if len(list) != 1 || list[0].ID != c.ID {
		t.Fatalf("expected 1 contact, got %d", len(list))
	}
	// Search hit + miss.
	if hits, _ := s.ListContactsForTenant(ctx, ten.ID, "Ada"); len(hits) != 1 {
		t.Errorf("search 'Ada' expected 1, got %d", len(hits))
	}
	if miss, _ := s.ListContactsForTenant(ctx, ten.ID, "zzzzz"); len(miss) != 0 {
		t.Errorf("search miss expected 0, got %d", len(miss))
	}
	if err := s.DeleteContactForTenant(ctx, ten.ID, c.ID); err != nil {
		t.Fatalf("DeleteContact: %v", err)
	}
	if after, _ := s.ListContactsForTenant(ctx, ten.ID, ""); len(after) != 0 {
		t.Errorf("expected 0 after delete, got %d", len(after))
	}
}

func TestIntegrationWebhookCRUD(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)

	ep, err := s.CreateWebhookEndpoint(ctx, ten.ID, "https://example.com/hook", "secret123", []string{"call.completed"})
	if err != nil {
		t.Fatalf("CreateWebhookEndpoint: %v", err)
	}
	eps, err := s.ListWebhookEndpointsForTenant(ctx, ten.ID)
	if err != nil {
		t.Fatalf("ListWebhookEndpoints: %v", err)
	}
	if len(eps) != 1 || eps[0].ID != ep.ID {
		t.Fatalf("expected 1 endpoint, got %d", len(eps))
	}
	// nil events must be coerced to a non-NULL empty slice (regression guard).
	ep2, err := s.CreateWebhookEndpoint(ctx, ten.ID, "https://example.com/all", "s2", nil)
	if err != nil {
		t.Fatalf("CreateWebhookEndpoint(nil events): %v", err)
	}
	if ep2.Events == nil {
		t.Error("nil events should be coerced to empty slice, got nil")
	}
	if err := s.DeleteWebhookEndpointForTenant(ctx, ten.ID, ep.ID); err != nil {
		t.Fatalf("DeleteWebhookEndpoint: %v", err)
	}
}
