//go:build integration

package store

import (
	"context"
	"errors"
	"testing"
)

func TestIntegrationParkLotLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)
	// A sip_domain is needed for the dialplan lookups.
	_, domain := makeExtension(t, s, ten, "1001")

	lot, err := s.CreateParkLot(ctx, CreateParkLotInput{
		TenantID: ten.ID, Name: "Front desk", FeatureCode: "*68",
		SlotStart: 700, SlotEnd: 779,
	})
	if err != nil {
		t.Fatalf("CreateParkLot: %v", err)
	}
	if lot.FeatureCode != "*68" || lot.SlotStart != 700 || lot.SlotEnd != 779 || !lot.Enabled {
		t.Fatalf("unexpected lot: %+v", lot)
	}

	// List shows it.
	lots, err := s.ListParkLotsForTenant(ctx, ten.ID)
	if err != nil || len(lots) != 1 {
		t.Fatalf("ListParkLots: %d lots, err %v", len(lots), err)
	}

	// Get enforces tenant ownership.
	got, err := s.GetParkLotForTenant(ctx, ten.ID, lot.ID)
	if err != nil || got.ID != lot.ID {
		t.Fatalf("GetParkLotForTenant: %v", err)
	}

	// Feature-code lookup resolves the park code to the enabled lot.
	byCode, err := s.LookupParkLotByFeatureCode(ctx, domain, "*68")
	if err != nil || byCode.ID != lot.ID {
		t.Fatalf("LookupParkLotByFeatureCode: id %v err %v", byCode, err)
	}

	// Slot lookup resolves an in-range slot; out-of-range misses.
	bySlot, err := s.LookupParkLotBySlot(ctx, domain, 703)
	if err != nil || bySlot.ID != lot.ID {
		t.Fatalf("LookupParkLotBySlot(703): id %v err %v", bySlot, err)
	}
	if _, err := s.LookupParkLotBySlot(ctx, domain, 800); err == nil {
		t.Error("out-of-range slot should not resolve")
	}

	// Toggle disabled → lookups (enabled-only) now miss.
	if err := s.SetParkLotEnabled(ctx, ten.ID, lot.ID, false); err != nil {
		t.Fatalf("SetParkLotEnabled: %v", err)
	}
	if _, err := s.LookupParkLotByFeatureCode(ctx, domain, "*68"); err == nil {
		t.Error("disabled lot should not resolve via feature-code lookup")
	}
	if _, err := s.LookupParkLotBySlot(ctx, domain, 703); err == nil {
		t.Error("disabled lot should not resolve via slot lookup")
	}

	// Delete.
	if err := s.DeleteParkLotForTenant(ctx, ten.ID, lot.ID); err != nil {
		t.Fatalf("DeleteParkLot: %v", err)
	}
	if _, err := s.GetParkLotForTenant(ctx, ten.ID, lot.ID); err == nil {
		t.Error("lot should be gone after delete")
	}
}

func TestIntegrationParkLotCrossTenantGuard(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tenA := makeTenant(t, s)
	tenB := makeTenant(t, s)

	lotA, err := s.CreateParkLot(ctx, CreateParkLotInput{
		TenantID: tenA.ID, Name: "A lot", FeatureCode: "*68",
		SlotStart: 700, SlotEnd: 779,
	})
	if err != nil {
		t.Fatalf("CreateParkLot: %v", err)
	}

	// Tenant B cannot fetch A's lot.
	if _, err := s.GetParkLotForTenant(ctx, tenB.ID, lotA.ID); err == nil {
		t.Error("cross-tenant get should fail")
	}
	// Tenant B cannot toggle A's lot.
	if err := s.SetParkLotEnabled(ctx, tenB.ID, lotA.ID, false); !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("cross-tenant toggle: want ErrCrossTenant, got %v", err)
	}
	// Tenant B cannot delete A's lot.
	if err := s.DeleteParkLotForTenant(ctx, tenB.ID, lotA.ID); !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("cross-tenant delete: want ErrCrossTenant, got %v", err)
	}
}
