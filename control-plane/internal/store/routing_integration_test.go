//go:build integration

package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// makeCarrierAccount creates a tenant-owned trunk against the first seeded
// carrier (CallCentric). Skips the test if no carriers are seeded.
func makeCarrierAccount(t *testing.T, s *Store, ten *Tenant) *CarrierAccount {
	t.Helper()
	ctx := context.Background()
	carriers, err := s.ListCarriers(ctx)
	if err != nil {
		t.Fatalf("ListCarriers: %v", err)
	}
	if len(carriers) == 0 {
		t.Skip("no seeded carriers")
	}
	ca, err := s.CreateCarrierAccount(ctx, CreateCarrierAccountInput{
		TenantID: &ten.ID, CarrierID: carriers[0].ID, Name: "trunk",
		SIPUsername: "u", SIPPassword: "p", FSGatewayName: "gw-" + uuid.NewString()[:8], Register: true,
	})
	if err != nil {
		t.Fatalf("CreateCarrierAccount: %v", err)
	}
	return ca
}

func TestIntegrationOutboundRouteLongestPrefix(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)
	ca := makeCarrierAccount(t, s, ten)

	// A broad "+1" route and a specific "+1818" route to the same trunk.
	broad, err := s.CreateOutboundRoute(ctx, CreateOutboundRouteInput{
		TenantID: ten.ID, Name: "NANP", MatchPrefix: "+1", CarrierAccountID: ca.ID,
	})
	if err != nil {
		t.Fatalf("CreateOutboundRoute broad: %v", err)
	}
	la, err := s.CreateOutboundRoute(ctx, CreateOutboundRouteInput{
		TenantID: ten.ID, Name: "LA", MatchPrefix: "+1818", CarrierAccountID: ca.ID,
		CallerIDE164: "+18185550000",
	})
	if err != nil {
		t.Fatalf("CreateOutboundRoute la: %v", err)
	}

	// An LA number must take the longer, more-specific route (+ its caller ID).
	dec, err := s.ResolveOutboundRouteForTenant(ctx, ten.ID, "+18185551234")
	if err != nil {
		t.Fatalf("Resolve LA: %v", err)
	}
	if dec.RouteID != la.ID {
		t.Errorf("LA number resolved to %v, want the +1818 route %v", dec.RouteID, la.ID)
	}
	if dec.CIDOverride != "+18185550000" {
		t.Errorf("caller-id override = %q, want +18185550000", dec.CIDOverride)
	}

	// A different NANP number falls to the broad route.
	dec, err = s.ResolveOutboundRouteForTenant(ctx, ten.ID, "+12125551234")
	if err != nil {
		t.Fatalf("Resolve NY: %v", err)
	}
	if dec.RouteID != broad.ID {
		t.Errorf("NY number resolved to %v, want the +1 route %v", dec.RouteID, broad.ID)
	}

	// A non-matching number resolves nothing.
	if _, err := s.ResolveOutboundRouteForTenant(ctx, ten.ID, "+442071234567"); err == nil {
		t.Error("non-matching number should not resolve a route")
	}
}

func TestIntegrationWebhookDeliveryStatusAndToggle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)

	ep, err := s.CreateWebhookEndpoint(ctx, ten.ID, "https://example.com/hook", "sec", []string{"call.completed"})
	if err != nil {
		t.Fatalf("CreateWebhookEndpoint: %v", err)
	}

	// Record a delivery → last_status surfaces in the list.
	if err := s.RecordWebhookDelivery(ctx, ep.ID, "ok", ""); err != nil {
		t.Fatalf("RecordWebhookDelivery: %v", err)
	}
	eps, _ := s.ListWebhookEndpointsForTenant(ctx, ten.ID)
	if len(eps) != 1 || eps[0].LastStatus == nil || *eps[0].LastStatus != "ok" {
		t.Fatalf("last status not recorded: %+v", eps)
	}

	// Disable → reflected in the list.
	if err := s.SetWebhookEnabled(ctx, ten.ID, ep.ID, false); err != nil {
		t.Fatalf("SetWebhookEnabled: %v", err)
	}
	eps, _ = s.ListWebhookEndpointsForTenant(ctx, ten.ID)
	if eps[0].Enabled {
		t.Error("endpoint should be disabled")
	}

	// Rotate secret → new secret stored.
	if err := s.RotateWebhookSecret(ctx, ten.ID, ep.ID, "newsecret"); err != nil {
		t.Fatalf("RotateWebhookSecret: %v", err)
	}
	eps, _ = s.ListWebhookEndpointsForTenant(ctx, ten.ID)
	if eps[0].Secret != "newsecret" {
		t.Errorf("secret = %q, want newsecret", eps[0].Secret)
	}
}

func TestIntegrationTenantAlertEmail(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)

	if err := s.UpdateTenantAlertEmail(ctx, ten.ID, "alerts@example.com"); err != nil {
		t.Fatalf("UpdateTenantAlertEmail: %v", err)
	}
	got, err := s.GetTenant(ctx, ten.ID)
	if err != nil {
		t.Fatalf("GetTenant: %v", err)
	}
	if got.AlertEmail != "alerts@example.com" {
		t.Errorf("alert email = %q, want alerts@example.com", got.AlertEmail)
	}
}
