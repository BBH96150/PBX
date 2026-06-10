//go:build integration

package store

import (
	"context"
	"errors"
	"testing"
)

// makeQueue creates a simple enabled queue for a tenant.
func makeQueue(t *testing.T, s *Store, ten *Tenant, ext string) *Queue {
	t.Helper()
	q, err := s.CreateQueue(context.Background(), CreateQueueInput{
		TenantID: ten.ID, Extension: ext, Name: "Q " + ext,
	})
	if err != nil {
		t.Fatalf("CreateQueue: %v", err)
	}
	return q
}

func TestIntegrationQueueCallbackLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)
	q := makeQueue(t, s, ten, "700")

	// Create via the dialplan-facing helper.
	cb, err := s.RequestQueueCallback(ctx, ten.ID, q.ID, "+14155550123", "Ada")
	if err != nil {
		t.Fatalf("RequestQueueCallback: %v", err)
	}
	if cb.Status != "pending" {
		t.Fatalf("new callback status = %q, want pending", cb.Status)
	}
	if cb.Attempts != 0 {
		t.Fatalf("new callback attempts = %d, want 0", cb.Attempts)
	}
	if cb.CallerName != "Ada" {
		t.Fatalf("caller name = %q, want Ada", cb.CallerName)
	}
	// last_attempt_at must be NULL on a fresh row (the nullable-scan gotcha).
	if cb.LastAttemptAt != nil {
		t.Fatalf("fresh callback last_attempt_at should be nil, got %v", cb.LastAttemptAt)
	}

	// List (all) + list (pending) + list (filtered) — the join surfaces the queue
	// extension.
	all, err := s.ListQueueCallbacksForTenant(ctx, ten.ID, "")
	if err != nil || len(all) != 1 {
		t.Fatalf("ListQueueCallbacksForTenant: %d rows, err %v", len(all), err)
	}
	if all[0].QueueExtension != "700" {
		t.Fatalf("joined queue extension = %q, want 700", all[0].QueueExtension)
	}
	pending, err := s.ListPendingQueueCallbacks(ctx, ten.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("ListPendingQueueCallbacks: %d rows, err %v", len(pending), err)
	}
	if filtered, _ := s.ListQueueCallbacksForTenant(ctx, ten.ID, "connected"); len(filtered) != 0 {
		t.Fatalf("status filter 'connected' should be empty, got %d", len(filtered))
	}

	// Get enforces tenant ownership.
	got, err := s.GetQueueCallbackForTenant(ctx, ten.ID, cb.ID)
	if err != nil || got.ID != cb.ID {
		t.Fatalf("GetQueueCallbackForTenant: %v", err)
	}

	// Status transitions: pending → dialing (attempt++ + last_attempt_at) →
	// connected.
	if err := s.IncrementQueueCallbackAttempt(ctx, ten.ID, cb.ID, "dialing"); err != nil {
		t.Fatalf("IncrementQueueCallbackAttempt: %v", err)
	}
	got, _ = s.GetQueueCallbackForTenant(ctx, ten.ID, cb.ID)
	if got.Status != "dialing" || got.Attempts != 1 {
		t.Fatalf("after attempt: status=%q attempts=%d, want dialing/1", got.Status, got.Attempts)
	}
	if got.LastAttemptAt == nil {
		t.Fatalf("last_attempt_at should be set after an attempt")
	}

	if err := s.SetQueueCallbackStatus(ctx, ten.ID, cb.ID, "connected"); err != nil {
		t.Fatalf("SetQueueCallbackStatus: %v", err)
	}
	got, _ = s.GetQueueCallbackForTenant(ctx, ten.ID, cb.ID)
	if got.Status != "connected" {
		t.Fatalf("status after SetQueueCallbackStatus = %q, want connected", got.Status)
	}

	// Now that it's connected it's no longer pending.
	if pending, _ := s.ListPendingQueueCallbacks(ctx, ten.ID); len(pending) != 0 {
		t.Fatalf("connected callback should not be pending, got %d pending", len(pending))
	}

	// Cancel sets the terminal cancelled status.
	if err := s.CancelQueueCallback(ctx, ten.ID, cb.ID); err != nil {
		t.Fatalf("CancelQueueCallback: %v", err)
	}
	got, _ = s.GetQueueCallbackForTenant(ctx, ten.ID, cb.ID)
	if got.Status != "cancelled" {
		t.Fatalf("status after cancel = %q, want cancelled", got.Status)
	}
}

func TestIntegrationQueueCallbackCrossTenantGuard(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tenA := makeTenant(t, s)
	tenB := makeTenant(t, s)
	qA := makeQueue(t, s, tenA, "700")

	cbA, err := s.RequestQueueCallback(ctx, tenA.ID, qA.ID, "+14155550000", "")
	if err != nil {
		t.Fatalf("RequestQueueCallback: %v", err)
	}
	// Empty caller name persists as NULL → COALESCE'd back to "" (nullable scan).
	if cbA.CallerName != "" {
		t.Fatalf("empty caller name should round-trip as empty, got %q", cbA.CallerName)
	}

	// Tenant B cannot see A's callback.
	if _, err := s.GetQueueCallbackForTenant(ctx, tenB.ID, cbA.ID); err == nil {
		t.Error("cross-tenant get should fail")
	}
	if list, _ := s.ListQueueCallbacksForTenant(ctx, tenB.ID, ""); len(list) != 0 {
		t.Errorf("tenant B should see 0 callbacks, got %d", len(list))
	}

	// Tenant B cannot mutate A's callback.
	if err := s.SetQueueCallbackStatus(ctx, tenB.ID, cbA.ID, "connected"); !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("cross-tenant status: want ErrCrossTenant, got %v", err)
	}
	if err := s.IncrementQueueCallbackAttempt(ctx, tenB.ID, cbA.ID, "dialing"); !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("cross-tenant attempt: want ErrCrossTenant, got %v", err)
	}
	if err := s.CancelQueueCallback(ctx, tenB.ID, cbA.ID); !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("cross-tenant cancel: want ErrCrossTenant, got %v", err)
	}

	// A cross-tenant queue_id cannot be linked at create time (subquery yields
	// NULL → NOT NULL violation).
	if _, err := s.RequestQueueCallback(ctx, tenB.ID, qA.ID, "+14155551111", ""); err == nil {
		t.Error("creating a callback against another tenant's queue should fail")
	}
}
