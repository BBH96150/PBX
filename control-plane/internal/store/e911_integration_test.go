//go:build integration

package store

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestIntegrationE911LocationLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)

	loc, err := s.CreateE911Location(ctx, CreateE911LocationInput{
		TenantID: ten.ID, Label: "HQ 3rd floor", Street: "123 Main St",
		Street2: "Suite 400", City: "Austin", Region: "TX", PostalCode: "78701",
	})
	if err != nil {
		t.Fatalf("CreateE911Location: %v", err)
	}
	if loc.Country != "US" {
		t.Fatalf("default country: want US, got %q", loc.Country)
	}
	if !loc.Enabled {
		t.Fatalf("new location should be enabled")
	}
	if want := "123 Main St, Suite 400, Austin, TX, 78701, US"; loc.SingleLine() != want {
		t.Fatalf("SingleLine = %q, want %q", loc.SingleLine(), want)
	}

	// List shows it.
	locs, err := s.ListE911LocationsForTenant(ctx, ten.ID)
	if err != nil || len(locs) != 1 {
		t.Fatalf("ListE911LocationsForTenant: %d locs, err %v", len(locs), err)
	}

	// Get enforces tenant ownership.
	got, err := s.GetE911LocationForTenant(ctx, ten.ID, loc.ID)
	if err != nil || got.ID != loc.ID {
		t.Fatalf("GetE911LocationForTenant: %v", err)
	}

	// Toggle disabled.
	if err := s.SetE911LocationEnabled(ctx, ten.ID, loc.ID, false); err != nil {
		t.Fatalf("SetE911LocationEnabled: %v", err)
	}
	got, _ = s.GetE911LocationForTenant(ctx, ten.ID, loc.ID)
	if got.Enabled {
		t.Fatalf("location should be disabled after toggle")
	}

	// Delete.
	if err := s.DeleteE911LocationForTenant(ctx, ten.ID, loc.ID); err != nil {
		t.Fatalf("DeleteE911LocationForTenant: %v", err)
	}
	if _, err := s.GetE911LocationForTenant(ctx, ten.ID, loc.ID); err == nil {
		t.Error("location should be gone after delete")
	}
}

func TestIntegrationE911LocationCrossTenantGuard(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tenA := makeTenant(t, s)
	tenB := makeTenant(t, s)

	locA, err := s.CreateE911Location(ctx, CreateE911LocationInput{
		TenantID: tenA.ID, Label: "A site", Street: "1 A St",
		City: "Austin", Region: "TX", PostalCode: "78701",
	})
	if err != nil {
		t.Fatalf("CreateE911Location: %v", err)
	}

	// Tenant B cannot fetch A's location.
	if _, err := s.GetE911LocationForTenant(ctx, tenB.ID, locA.ID); err == nil {
		t.Error("cross-tenant get should fail")
	}
	// Tenant B cannot toggle A's location.
	if err := s.SetE911LocationEnabled(ctx, tenB.ID, locA.ID, false); !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("cross-tenant toggle: want ErrCrossTenant, got %v", err)
	}
	// Tenant B cannot delete A's location.
	if err := s.DeleteE911LocationForTenant(ctx, tenB.ID, locA.ID); !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("cross-tenant delete: want ErrCrossTenant, got %v", err)
	}
}

func TestIntegrationE911AssignAndResolve(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)
	ext, domain := makeExtension(t, s, ten, "6501")

	loc, err := s.CreateE911Location(ctx, CreateE911LocationInput{
		TenantID: ten.ID, Label: "Office", Street: "123 Main St",
		City: "Austin", Region: "TX", PostalCode: "78701",
	})
	if err != nil {
		t.Fatalf("CreateE911Location: %v", err)
	}

	// Before assignment: resolver returns the extension but no address.
	res, err := s.ResolveE911ForExtensionNumber(ctx, domain, "6501")
	if err != nil {
		t.Fatalf("ResolveE911ForExtensionNumber (unassigned): %v", err)
	}
	if res.TenantID != ten.ID || res.SIPUsername != ext.SIPUsername || res.Extension != "6501" {
		t.Fatalf("unexpected resolution: %+v", res)
	}
	if res.Address != nil {
		t.Fatalf("unassigned extension should resolve with nil address, got %+v", res.Address)
	}

	// Assign the location.
	if err := s.SetExtensionE911Location(ctx, ten.ID, ext.ID, &loc.ID); err != nil {
		t.Fatalf("SetExtensionE911Location: %v", err)
	}
	res, err = s.ResolveE911ForExtensionNumber(ctx, domain, "6501")
	if err != nil {
		t.Fatalf("ResolveE911ForExtensionNumber (assigned): %v", err)
	}
	if res.Address == nil || res.Address.ID != loc.ID {
		t.Fatalf("assigned extension should resolve the location, got %+v", res.Address)
	}
	if want := "123 Main St, Austin, TX, 78701, US"; res.Address.SingleLine() != want {
		t.Fatalf("resolved address = %q, want %q", res.Address.SingleLine(), want)
	}

	// Cross-tenant assignment is rejected (ext belongs to ten, not a stranger).
	other := makeTenant(t, s)
	if err := s.SetExtensionE911Location(ctx, other.ID, ext.ID, &loc.ID); !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("cross-tenant assign: want ErrCrossTenant, got %v", err)
	}

	// Clear the assignment (nil location).
	var nilLoc *uuid.UUID
	if err := s.SetExtensionE911Location(ctx, ten.ID, ext.ID, nilLoc); err != nil {
		t.Fatalf("SetExtensionE911Location(clear): %v", err)
	}
	res, _ = s.ResolveE911ForExtensionNumber(ctx, domain, "6501")
	if res.Address != nil {
		t.Fatalf("cleared extension should resolve nil address, got %+v", res.Address)
	}

	// Deleting the location detaches it (ON DELETE SET NULL) — assign first.
	if err := s.SetExtensionE911Location(ctx, ten.ID, ext.ID, &loc.ID); err != nil {
		t.Fatalf("re-assign: %v", err)
	}
	if err := s.DeleteE911LocationForTenant(ctx, ten.ID, loc.ID); err != nil {
		t.Fatalf("DeleteE911LocationForTenant: %v", err)
	}
	res, err = s.ResolveE911ForExtensionNumber(ctx, domain, "6501")
	if err != nil {
		t.Fatalf("resolve after location delete: %v", err)
	}
	if res.Address != nil {
		t.Fatalf("after location delete the extension should resolve nil address, got %+v", res.Address)
	}
}
