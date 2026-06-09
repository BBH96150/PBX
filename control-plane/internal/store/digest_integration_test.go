//go:build integration

package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func dueContains(list []DigestTenant, id uuid.UUID) bool {
	for _, d := range list {
		if d.ID == id {
			return true
		}
	}
	return false
}

func TestIntegrationDailyDigestScheduling(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)

	today := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	tomorrow := today.AddDate(0, 0, 1)

	// Digest is off by default → not due.
	due, err := s.ListDigestTenantsDue(ctx, today)
	if err != nil {
		t.Fatalf("ListDigestTenantsDue: %v", err)
	}
	if dueContains(due, ten.ID) {
		t.Fatal("tenant with digest off should not be due")
	}

	// Enable → now due (never sent).
	if err := s.UpdateTenantDigest(ctx, ten.ID, true); err != nil {
		t.Fatalf("UpdateTenantDigest: %v", err)
	}
	due, _ = s.ListDigestTenantsDue(ctx, today)
	if !dueContains(due, ten.ID) {
		t.Fatal("enabled tenant that has never been sent should be due")
	}

	// Mark sent today → no longer due today...
	if err := s.MarkDigestSent(ctx, ten.ID, today); err != nil {
		t.Fatalf("MarkDigestSent: %v", err)
	}
	due, _ = s.ListDigestTenantsDue(ctx, today)
	if dueContains(due, ten.ID) {
		t.Fatal("tenant already sent today should not be due today")
	}

	// ...but due again tomorrow.
	due, _ = s.ListDigestTenantsDue(ctx, tomorrow)
	if !dueContains(due, ten.ID) {
		t.Fatal("tenant should be due again the next day")
	}
}
