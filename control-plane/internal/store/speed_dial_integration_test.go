//go:build integration

package store

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// makeUser creates a tenant-scoped user for speed-dial ownership tests.
func makeUser(t *testing.T, s *Store, tenantID uuid.UUID) *User {
	t.Helper()
	u, err := s.CreateUser(context.Background(), CreateUserInput{
		TenantID:    &tenantID,
		Email:       "u-" + uuid.NewString()[:8] + "@example.com",
		DisplayName: "Test User",
		Password:    "password123",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return u
}

func TestIntegrationSpeedDialCRUD(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)
	u := makeUser(t, s, ten.ID)

	front, err := s.CreateSpeedDial(ctx, CreateSpeedDialInput{
		UserID: u.ID, TenantID: ten.ID, Label: "Front desk", Number: "1001", SortOrder: 1,
	})
	if err != nil {
		t.Fatalf("CreateSpeedDial: %v", err)
	}
	if front.Label != "Front desk" || front.Number != "1001" || front.UserID != u.ID || front.TenantID != ten.ID {
		t.Fatalf("unexpected speed dial: %+v", front)
	}

	mobile, err := s.CreateSpeedDial(ctx, CreateSpeedDialInput{
		UserID: u.ID, TenantID: ten.ID, Label: "Cell", Number: "+14155550100", SortOrder: 2,
	})
	if err != nil {
		t.Fatalf("CreateSpeedDial (2): %v", err)
	}

	list, err := s.ListSpeedDialsForUser(ctx, u.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListSpeedDialsForUser: %d dials, err %v", len(list), err)
	}
	// sort_order ordering: Front desk (1) before Cell (2).
	if list[0].ID != front.ID || list[1].ID != mobile.ID {
		t.Fatalf("unexpected order: %+v", list)
	}

	// Delete one → only the other remains.
	if err := s.DeleteSpeedDialForUser(ctx, u.ID, front.ID); err != nil {
		t.Fatalf("DeleteSpeedDialForUser: %v", err)
	}
	after, _ := s.ListSpeedDialsForUser(ctx, u.ID)
	if len(after) != 1 || after[0].ID != mobile.ID {
		t.Fatalf("expected only Cell after delete, got %+v", after)
	}
}

func TestIntegrationSpeedDialUserScoping(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)
	alice := makeUser(t, s, ten.ID)
	bob := makeUser(t, s, ten.ID)

	aliceDial, err := s.CreateSpeedDial(ctx, CreateSpeedDialInput{
		UserID: alice.ID, TenantID: ten.ID, Label: "Boss", Number: "2002",
	})
	if err != nil {
		t.Fatalf("CreateSpeedDial: %v", err)
	}

	// Bob (same tenant, different user) cannot see Alice's speed dial.
	bobList, _ := s.ListSpeedDialsForUser(ctx, bob.ID)
	if len(bobList) != 0 {
		t.Fatalf("bob should see no speed dials, got %d", len(bobList))
	}

	// Bob cannot delete Alice's speed dial — user_id-scoped no-op → ErrCrossTenant.
	if err := s.DeleteSpeedDialForUser(ctx, bob.ID, aliceDial.ID); !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("cross-user delete: want ErrCrossTenant, got %v", err)
	}

	// Alice's dial is untouched.
	aliceList, _ := s.ListSpeedDialsForUser(ctx, alice.ID)
	if len(aliceList) != 1 || aliceList[0].ID != aliceDial.ID {
		t.Fatalf("alice's dial should be intact, got %+v", aliceList)
	}

	// Alice can delete her own.
	if err := s.DeleteSpeedDialForUser(ctx, alice.ID, aliceDial.ID); err != nil {
		t.Fatalf("owner delete: %v", err)
	}
}
