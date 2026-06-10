//go:build integration

package store

import (
	"context"
	"errors"
	"testing"
)

func TestIntegrationBlockedNumberLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)
	// A primary sip_domain is needed for the IsCallerBlocked lookup.
	_, domain := makeExtension(t, s, ten, "1001")

	bn, err := s.CreateBlockedNumber(ctx, CreateBlockedNumberInput{
		TenantID: ten.ID, Number: "+15551234567", Label: "spam",
	})
	if err != nil {
		t.Fatalf("CreateBlockedNumber: %v", err)
	}
	if bn.Number != "+15551234567" || bn.Label != "spam" {
		t.Fatalf("unexpected blocked number: %+v", bn)
	}

	// List shows it.
	nums, err := s.ListBlockedNumbersForTenant(ctx, ten.ID)
	if err != nil || len(nums) != 1 {
		t.Fatalf("ListBlockedNumbers: %d numbers, err %v", len(nums), err)
	}

	// A number with no label scans NULL→"" safely.
	bn2, err := s.CreateBlockedNumber(ctx, CreateBlockedNumberInput{
		TenantID: ten.ID, Number: "+15559998888",
	})
	if err != nil {
		t.Fatalf("CreateBlockedNumber (no label): %v", err)
	}
	if bn2.Label != "" {
		t.Fatalf("expected empty label, got %q", bn2.Label)
	}

	// IsCallerBlocked: exact match blocks.
	blocked, err := s.IsCallerBlocked(ctx, domain, "+15551234567")
	if err != nil {
		t.Fatalf("IsCallerBlocked exact: %v", err)
	}
	if !blocked {
		t.Error("exact caller should be blocked")
	}

	// Last-10-digit match: trunk presents bare 10 digits → still blocked.
	blocked, err = s.IsCallerBlocked(ctx, domain, "5551234567")
	if err != nil {
		t.Fatalf("IsCallerBlocked last10: %v", err)
	}
	if !blocked {
		t.Error("bare-10-digit caller should match the +1 blocked number")
	}

	// Last-10-digit match: trunk presents 1XXXXXXXXXX → still blocked.
	blocked, err = s.IsCallerBlocked(ctx, domain, "15551234567")
	if err != nil {
		t.Fatalf("IsCallerBlocked 1-prefixed: %v", err)
	}
	if !blocked {
		t.Error("1-prefixed caller should match the +1 blocked number")
	}

	// A different caller is NOT blocked.
	blocked, err = s.IsCallerBlocked(ctx, domain, "+14155550000")
	if err != nil {
		t.Fatalf("IsCallerBlocked miss: %v", err)
	}
	if blocked {
		t.Error("unlisted caller should not be blocked")
	}

	// Empty caller / domain → never blocked, never error.
	if b, err := s.IsCallerBlocked(ctx, "", "+15551234567"); err != nil || b {
		t.Errorf("empty domain: blocked=%v err=%v", b, err)
	}
	if b, err := s.IsCallerBlocked(ctx, domain, ""); err != nil || b {
		t.Errorf("empty caller: blocked=%v err=%v", b, err)
	}

	// Delete → no longer blocked.
	if err := s.DeleteBlockedNumberForTenant(ctx, ten.ID, bn.ID); err != nil {
		t.Fatalf("DeleteBlockedNumber: %v", err)
	}
	blocked, err = s.IsCallerBlocked(ctx, domain, "+15551234567")
	if err != nil {
		t.Fatalf("IsCallerBlocked after delete: %v", err)
	}
	if blocked {
		t.Error("deleted number should no longer block")
	}
}

func TestIntegrationBlockedNumberCrossTenantGuard(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tenA := makeTenant(t, s)
	tenB := makeTenant(t, s)
	_, domainA := makeExtension(t, s, tenA, "1001")
	_, domainB := makeExtension(t, s, tenB, "2001")

	bnA, err := s.CreateBlockedNumber(ctx, CreateBlockedNumberInput{
		TenantID: tenA.ID, Number: "+15551234567", Label: "spam",
	})
	if err != nil {
		t.Fatalf("CreateBlockedNumber: %v", err)
	}

	// Tenant A's block does NOT apply to tenant B's domain.
	if blocked, err := s.IsCallerBlocked(ctx, domainB, "+15551234567"); err != nil || blocked {
		t.Errorf("tenant B should not be affected by tenant A's block: blocked=%v err=%v", blocked, err)
	}
	// But it does apply to tenant A's own domain.
	if blocked, err := s.IsCallerBlocked(ctx, domainA, "+15551234567"); err != nil || !blocked {
		t.Errorf("tenant A's own domain should block: blocked=%v err=%v", blocked, err)
	}

	// Tenant B cannot delete A's blocked number.
	if err := s.DeleteBlockedNumberForTenant(ctx, tenB.ID, bnA.ID); !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("cross-tenant delete: want ErrCrossTenant, got %v", err)
	}
}
