//go:build integration

package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestIntegrationDIDToExtension(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)
	ext, _ := makeExtension(t, s, ten, "9001")

	carriers, err := s.ListCarriers(ctx)
	if err != nil {
		t.Fatalf("ListCarriers: %v", err)
	}
	if len(carriers) == 0 {
		t.Skip("no seeded carriers (expected CallCentric from migration 0002)")
	}

	// Globally-unique, well-formed E164.
	e164 := fmt.Sprintf("+1415555%04d", time.Now().UnixNano()%10000)
	did, err := s.CreateDID(ctx, CreateDIDInput{
		TenantID: ten.ID, CarrierID: carriers[0].ID, E164: e164,
		DestinationKind: "extension", DestinationID: ext.ID,
	})
	if err != nil {
		t.Fatalf("CreateDID: %v", err)
	}
	if did.E164 != e164 || did.DestinationKind != "extension" {
		t.Fatalf("unexpected DID: %+v", did)
	}

	dids, err := s.ListDIDsForTenant(ctx, ten.ID)
	if err != nil {
		t.Fatalf("ListDIDsForTenant: %v", err)
	}
	found := false
	for _, d := range dids {
		if d.ID == did.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("created DID not in tenant list")
	}
}

func TestIntegrationVoicemailBox(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)
	ext, _ := makeExtension(t, s, ten, "9101")

	box, err := s.CreateVoicemailBox(ctx, CreateVoicemailBoxInput{
		TenantID: ten.ID, ExtensionID: ext.ID, PIN: "1234", Email: "vm@example.com",
	})
	if err != nil {
		t.Fatalf("CreateVoicemailBox: %v", err)
	}

	got, err := s.GetVoicemailBoxByExtensionID(ctx, ext.ID)
	if err != nil {
		t.Fatalf("GetVoicemailBoxByExtensionID: %v", err)
	}
	if got.ID != box.ID {
		t.Fatalf("voicemail box mismatch: %v vs %v", got.ID, box.ID)
	}
	// New box defaults: VM-to-email opt-in off, no address (migration 0043).
	if got.EmailEnabled || got.EmailAddress != "" {
		t.Fatalf("new box should default email-notify off/empty, got enabled=%v addr=%q",
			got.EmailEnabled, got.EmailAddress)
	}
}

func TestIntegrationVoicemailEmailNotify(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)
	ext, dom := makeExtension(t, s, ten, "9201")

	if _, err := s.CreateVoicemailBox(ctx, CreateVoicemailBoxInput{
		TenantID: ten.ID, ExtensionID: ext.ID, PIN: "1234",
	}); err != nil {
		t.Fatalf("CreateVoicemailBox: %v", err)
	}

	// Enable + set an address.
	if err := s.UpdateVoicemailEmailNotify(ctx, ext.ID, true, "owner@example.com"); err != nil {
		t.Fatalf("UpdateVoicemailEmailNotify enable: %v", err)
	}
	got, err := s.GetVoicemailBoxByExtensionID(ctx, ext.ID)
	if err != nil {
		t.Fatalf("GetVoicemailBoxByExtensionID: %v", err)
	}
	if !got.EmailEnabled || got.EmailAddress != "owner@example.com" {
		t.Fatalf("after enable: got enabled=%v addr=%q", got.EmailEnabled, got.EmailAddress)
	}

	// Clearing the address forces enabled=false (never persist "on, nowhere").
	if err := s.UpdateVoicemailEmailNotify(ctx, ext.ID, true, ""); err != nil {
		t.Fatalf("UpdateVoicemailEmailNotify clear: %v", err)
	}
	got, err = s.GetVoicemailBoxByExtensionID(ctx, ext.ID)
	if err != nil {
		t.Fatalf("GetVoicemailBoxByExtensionID: %v", err)
	}
	if got.EmailEnabled || got.EmailAddress != "" {
		t.Fatalf("after clear: expected disabled+empty, got enabled=%v addr=%q",
			got.EmailEnabled, got.EmailAddress)
	}

	// The resolver used by the ESL handler returns the new fields too.
	rbox, err := s.GetVoicemailBoxByUserDomain(ctx, ext.SIPUsername, dom)
	if err != nil {
		t.Fatalf("GetVoicemailBoxByUserDomain: %v", err)
	}
	if rbox.EmailEnabled || rbox.EmailAddress != "" {
		t.Fatalf("resolver fields mismatch: enabled=%v addr=%q", rbox.EmailEnabled, rbox.EmailAddress)
	}

	// Tenant scoping: an extension in a different tenant has no box → not found.
	ten2 := makeTenant(t, s)
	ext2, _ := makeExtension(t, s, ten2, "9202")
	if err := s.UpdateVoicemailEmailNotify(ctx, ext2.ID, true, "x@example.com"); err != ErrVoicemailBoxNotFound {
		t.Fatalf("expected ErrVoicemailBoxNotFound for box-less extension, got %v", err)
	}
}
