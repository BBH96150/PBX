//go:build integration

package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestIntegrationInviteCreateAndList(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)

	email := "invitee-" + uuid.NewString()[:8] + "@example.com"
	inv, err := s.CreateInvite(ctx, CreateInviteInput{TenantID: ten.ID, Email: email, Role: "user"})
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if inv.Plaintext == "" {
		t.Error("invite should carry a plaintext token")
	}
	list, err := s.ListInvitesForTenant(ctx, ten.ID)
	if err != nil {
		t.Fatalf("ListInvitesForTenant: %v", err)
	}
	found := false
	for _, iv := range list {
		if iv.Email == email {
			found = true
		}
	}
	if !found {
		t.Fatal("created invite not in tenant list")
	}
}

func TestIntegrationAPITokenLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)

	tok, err := s.CreateAPIToken(ctx, CreateAPITokenInput{TenantID: &ten.ID, Name: "ci-key", Scope: "read"})
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	// The plaintext authenticates.
	if _, err := s.VerifyAPIToken(ctx, tok.Plaintext); err != nil {
		t.Fatalf("VerifyAPIToken before revoke: %v", err)
	}

	// It appears in the tenant's token list.
	toks, err := s.ListAPITokensForTenant(ctx, ten.ID)
	if err != nil {
		t.Fatalf("ListAPITokensForTenant: %v", err)
	}
	if len(toks) == 0 {
		t.Fatal("expected at least one token in the list")
	}

	// After revoke, the plaintext no longer authenticates.
	if err := s.RevokeAPITokenForTenant(ctx, ten.ID, tok.ID); err != nil {
		t.Fatalf("RevokeAPITokenForTenant: %v", err)
	}
	if _, err := s.VerifyAPIToken(ctx, tok.Plaintext); err == nil {
		t.Fatal("revoked token should no longer verify")
	}
}

func TestIntegrationUsersForTenant(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)

	email := "member-" + uuid.NewString()[:8] + "@example.com"
	u, err := s.CreateUser(ctx, CreateUserInput{
		TenantID: &ten.ID, Email: email, DisplayName: "Member", Role: "user", Password: "pw-secret-123",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// ListUsersForTenant joins user_tenant_memberships, so the membership must
	// exist (CreateUser sets users.tenant_id but does not add a membership row).
	if err := s.AddMembership(ctx, u.ID, ten.ID, "user"); err != nil {
		t.Fatalf("AddMembership: %v", err)
	}
	users, err := s.ListUsersForTenant(ctx, ten.ID)
	if err != nil {
		t.Fatalf("ListUsersForTenant: %v", err)
	}
	found := false
	for _, u := range users {
		if u.Email == email {
			found = true
		}
	}
	if !found {
		t.Fatalf("created user %q not listed for tenant", email)
	}
}
