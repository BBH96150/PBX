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
}
