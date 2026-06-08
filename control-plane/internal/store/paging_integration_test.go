//go:build integration

package store

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// makeExtension creates a sip_domain + extension under a tenant and returns
// both the extension and its domain (for dialplan-lookup tests).
func makeExtension(t *testing.T, s *Store, ten *Tenant, number string) (*Extension, string) {
	t.Helper()
	ctx := context.Background()
	domain := "it-" + uuid.NewString()[:8] + ".sip.local"
	sd, err := s.CreateSIPDomain(ctx, ten.ID, domain, true)
	if err != nil {
		t.Fatalf("CreateSIPDomain: %v", err)
	}
	ext, err := s.CreateExtension(ctx, ten.ID, sd.ID, number, number, "pw-"+number, "User "+number)
	if err != nil {
		t.Fatalf("CreateExtension: %v", err)
	}
	return ext, domain
}

func TestIntegrationPagingGroupLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)
	ext, domain := makeExtension(t, s, ten, "1001")

	g, err := s.CreatePagingGroup(ctx, CreatePagingGroupInput{
		TenantID: ten.ID, Name: "All staff", Extension: "800", Mode: "fs_conference",
	})
	if err != nil {
		t.Fatalf("CreatePagingGroup: %v", err)
	}
	if g.Mode != "fs_conference" || g.Extension != "800" || !g.Enabled {
		t.Fatalf("unexpected group: %+v", g)
	}

	// List shows it with a zero member count.
	groups, err := s.ListPagingGroupsForTenant(ctx, ten.ID)
	if err != nil || len(groups) != 1 {
		t.Fatalf("ListPagingGroups: %d groups, err %v", len(groups), err)
	}
	if groups[0].MemberCount != 0 {
		t.Errorf("member count = %d, want 0", groups[0].MemberCount)
	}

	// Add a member; idempotent re-add must not duplicate.
	if _, err := s.AddPagingMember(ctx, g.ID, ext.ID); err != nil {
		t.Fatalf("AddPagingMember: %v", err)
	}
	if _, err := s.AddPagingMember(ctx, g.ID, ext.ID); err != nil {
		t.Fatalf("AddPagingMember (idempotent): %v", err)
	}
	members, err := s.ListPagingMembersDetailed(ctx, g.ID)
	if err != nil || len(members) != 1 {
		t.Fatalf("members: %d, err %v", len(members), err)
	}
	if members[0].Extension != "1001" {
		t.Errorf("member extension = %q, want 1001", members[0].Extension)
	}

	// Dialplan lookup resolves the page code to the group + active members.
	info, err := s.LookupPagingGroupByExtension(ctx, domain, "800")
	if err != nil {
		t.Fatalf("LookupPagingGroupByExtension: %v", err)
	}
	if info.Group.ID != g.ID || len(info.Members) != 1 {
		t.Fatalf("lookup mismatch: group %v, %d members", info.Group.ID, len(info.Members))
	}

	// Toggle disabled → lookup (enabled-only) now misses.
	if err := s.SetPagingGroupEnabled(ctx, ten.ID, g.ID, false); err != nil {
		t.Fatalf("SetPagingGroupEnabled: %v", err)
	}
	if _, err := s.LookupPagingGroupByExtension(ctx, domain, "800"); err == nil {
		t.Error("disabled group should not resolve via lookup")
	}

	// Remove member.
	if err := s.RemovePagingMemberForTenant(ctx, ten.ID, members[0].ID); err != nil {
		t.Fatalf("RemovePagingMember: %v", err)
	}
	if after, _ := s.ListPagingMembersDetailed(ctx, g.ID); len(after) != 0 {
		t.Errorf("expected 0 members after remove, got %d", len(after))
	}

	// Delete the group.
	if err := s.DeletePagingGroupForTenant(ctx, ten.ID, g.ID); err != nil {
		t.Fatalf("DeletePagingGroup: %v", err)
	}
	if _, err := s.GetPagingGroupForTenant(ctx, ten.ID, g.ID); err == nil {
		t.Error("group should be gone after delete")
	}
}

func TestIntegrationPagingCrossTenantGuard(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tenA := makeTenant(t, s)
	tenB := makeTenant(t, s)
	extB, _ := makeExtension(t, s, tenB, "2002")

	gA, err := s.CreatePagingGroup(ctx, CreatePagingGroupInput{TenantID: tenA.ID, Name: "A", Extension: "801"})
	if err != nil {
		t.Fatalf("CreatePagingGroup: %v", err)
	}
	// Adding tenant B's extension to tenant A's group must be refused.
	if _, err := s.AddPagingMember(ctx, gA.ID, extB.ID); !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("cross-tenant add: want ErrCrossTenant, got %v", err)
	}
	// Deleting A's group as tenant B must be refused.
	if err := s.DeletePagingGroupForTenant(ctx, tenB.ID, gA.ID); !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("cross-tenant delete: want ErrCrossTenant, got %v", err)
	}
}
