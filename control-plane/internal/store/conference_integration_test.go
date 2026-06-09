//go:build integration

package store

import (
	"context"
	"errors"
	"testing"
)

func TestIntegrationConferenceRoomLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)
	// A sip_domain is needed for the extension-based dialplan lookup.
	_, domain := makeExtension(t, s, ten, "1001")

	room, err := s.CreateConferenceRoom(ctx, CreateConferenceRoomInput{
		TenantID: ten.ID, Name: "All hands", Extension: "8100",
		PIN: "1234", ModeratorPIN: "9999", MaxMembers: 25,
		Record: true, AnnounceCount: true,
	})
	if err != nil {
		t.Fatalf("CreateConferenceRoom: %v", err)
	}
	if room.Extension != "8100" || room.PIN != "1234" || room.ModeratorPIN != "9999" ||
		room.MaxMembers != 25 || !room.Record || !room.AnnounceCount || !room.Enabled {
		t.Fatalf("unexpected room: %+v", room)
	}

	// List shows it.
	rooms, err := s.ListConferenceRoomsForTenant(ctx, ten.ID)
	if err != nil || len(rooms) != 1 {
		t.Fatalf("ListConferenceRooms: %d rooms, err %v", len(rooms), err)
	}

	// Get enforces tenant ownership.
	got, err := s.GetConferenceRoomForTenant(ctx, ten.ID, room.ID)
	if err != nil || got.ID != room.ID {
		t.Fatalf("GetConferenceRoomForTenant: %v", err)
	}

	// Dialplan lookup resolves the room number to the enabled room.
	found, err := s.LookupConferenceRoomByExtension(ctx, domain, "8100")
	if err != nil {
		t.Fatalf("LookupConferenceRoomByExtension: %v", err)
	}
	if found.ID != room.ID {
		t.Fatalf("lookup mismatch: got %v, want %v", found.ID, room.ID)
	}

	// Toggle disabled → lookup (enabled-only) now misses.
	if err := s.SetConferenceRoomEnabled(ctx, ten.ID, room.ID, false); err != nil {
		t.Fatalf("SetConferenceRoomEnabled: %v", err)
	}
	if _, err := s.LookupConferenceRoomByExtension(ctx, domain, "8100"); err == nil {
		t.Error("disabled room should not resolve via lookup")
	}

	// Delete.
	if err := s.DeleteConferenceRoomForTenant(ctx, ten.ID, room.ID); err != nil {
		t.Fatalf("DeleteConferenceRoom: %v", err)
	}
	if _, err := s.GetConferenceRoomForTenant(ctx, ten.ID, room.ID); err == nil {
		t.Error("room should be gone after delete")
	}
}

func TestIntegrationConferenceRoomCrossTenantGuard(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tenA := makeTenant(t, s)
	tenB := makeTenant(t, s)

	roomA, err := s.CreateConferenceRoom(ctx, CreateConferenceRoomInput{
		TenantID: tenA.ID, Name: "A room", Extension: "8200",
	})
	if err != nil {
		t.Fatalf("CreateConferenceRoom: %v", err)
	}

	// Tenant B cannot fetch A's room.
	if _, err := s.GetConferenceRoomForTenant(ctx, tenB.ID, roomA.ID); err == nil {
		t.Error("cross-tenant get should fail")
	}
	// Tenant B cannot toggle A's room.
	if err := s.SetConferenceRoomEnabled(ctx, tenB.ID, roomA.ID, false); !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("cross-tenant toggle: want ErrCrossTenant, got %v", err)
	}
	// Tenant B cannot delete A's room.
	if err := s.DeleteConferenceRoomForTenant(ctx, tenB.ID, roomA.ID); !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("cross-tenant delete: want ErrCrossTenant, got %v", err)
	}
}
