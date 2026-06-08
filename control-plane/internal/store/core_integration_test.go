//go:build integration

package store

import (
	"context"
	"testing"
)

func TestIntegrationExtensionFeaturesAndOwner(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)
	ext, _ := makeExtension(t, s, ten, "3001")

	// Appears in the tenant's extension list.
	list, err := s.ListExtensionsForTenant(ctx, ten.ID)
	if err != nil {
		t.Fatalf("ListExtensionsForTenant: %v", err)
	}
	found := false
	for _, e := range list {
		if e.ID == ext.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("created extension not in tenant list")
	}

	// Toggle features.
	dnd := true
	cf := "+14155559999"
	updated, err := s.UpdateExtensionFeatures(ctx, ext.ID, UpdateExtensionFeaturesInput{
		DoNotDisturb: &dnd, CFImmediate: &cf,
	})
	if err != nil {
		t.Fatalf("UpdateExtensionFeatures: %v", err)
	}
	if !updated.DoNotDisturb || updated.CFImmediate != cf {
		t.Fatalf("features not applied: %+v", updated)
	}

	// Owner assign + clear (nil clears).
	if err := s.SetExtensionUser(ctx, ext.ID, nil); err != nil {
		t.Fatalf("SetExtensionUser(nil): %v", err)
	}
}

func TestIntegrationRingGroupRouting(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)
	ext, domain := makeExtension(t, s, ten, "4001")

	rg, err := s.CreateRingGroup(ctx, CreateRingGroupInput{
		TenantID: ten.ID, Extension: "600", Name: "Sales", Strategy: "simultaneous",
	})
	if err != nil {
		t.Fatalf("CreateRingGroup: %v", err)
	}
	if rg.Strategy != "simultaneous" || rg.RingTimeoutSec != 30 {
		t.Fatalf("ring group defaults wrong: %+v", rg)
	}

	if _, err := s.AddRingGroupMember(ctx, AddRingGroupMemberInput{RingGroupID: rg.ID, ExtensionID: ext.ID}); err != nil {
		t.Fatalf("AddRingGroupMember: %v", err)
	}

	// Dialplan lookup resolves the ring-group extension within the domain.
	info, err := s.LookupRingGroupByExtension(ctx, domain, "600")
	if err != nil {
		t.Fatalf("LookupRingGroupByExtension: %v", err)
	}
	if info.Group.ID != rg.ID {
		t.Fatalf("lookup returned wrong group: %v", info.Group.ID)
	}
	if len(info.Members) != 1 || info.Members[0].Extension != "4001" {
		t.Fatalf("expected member 4001, got %+v", info.Members)
	}
}

func TestIntegrationListsScopedToTenant(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tenA := makeTenant(t, s)
	tenB := makeTenant(t, s)
	makeExtension(t, s, tenA, "5001")

	// Tenant B sees none of tenant A's extensions.
	bList, err := s.ListExtensionsForTenant(ctx, tenB.ID)
	if err != nil {
		t.Fatalf("ListExtensionsForTenant(B): %v", err)
	}
	if len(bList) != 0 {
		t.Errorf("tenant B should have 0 extensions, got %d", len(bList))
	}
}
