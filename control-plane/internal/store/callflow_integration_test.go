//go:build integration

package store

import (
	"context"
	"testing"
)

func TestIntegrationQueueLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)
	ext, domain := makeExtension(t, s, ten, "7001")

	q, err := s.CreateQueue(ctx, CreateQueueInput{
		TenantID: ten.ID, Extension: "700", Name: "Support",
	})
	if err != nil {
		t.Fatalf("CreateQueue: %v", err)
	}
	if q.Extension != "700" {
		t.Fatalf("queue extension = %q", q.Extension)
	}

	if _, err := s.AddQueueAgent(ctx, AddQueueAgentInput{QueueID: q.ID, ExtensionID: ext.ID}); err != nil {
		t.Fatalf("AddQueueAgent: %v", err)
	}

	// Appears in the tenant list.
	qs, err := s.ListQueuesForTenant(ctx, ten.ID)
	if err != nil || len(qs) != 1 {
		t.Fatalf("ListQueuesForTenant: %d, err %v", len(qs), err)
	}

	// Dialplan lookup resolves the queue extension within the domain.
	got, err := s.LookupQueueByExtension(ctx, domain, "700")
	if err != nil {
		t.Fatalf("LookupQueueByExtension: %v", err)
	}
	if got.ID != q.ID {
		t.Fatalf("lookup returned wrong queue: %v", got.ID)
	}
}

func TestIntegrationIVRLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)
	ext, domain := makeExtension(t, s, ten, "8001")

	ivr, err := s.CreateIVR(ctx, CreateIVRInput{
		TenantID: ten.ID, Name: "Main menu", Extension: "900",
	})
	if err != nil {
		t.Fatalf("CreateIVR: %v", err)
	}

	// Option: press 1 → the extension.
	if _, err := s.AddIVROption(ctx, AddIVROptionInput{
		IVRID: ivr.ID, Digit: "1", ActionKind: "extension", ActionID: &ext.ID,
	}); err != nil {
		t.Fatalf("AddIVROption: %v", err)
	}

	got, err := s.LookupIVRByExtension(ctx, domain, "900")
	if err != nil {
		t.Fatalf("LookupIVRByExtension: %v", err)
	}
	if got.ID != ivr.ID {
		t.Fatalf("lookup returned wrong IVR: %v", got.ID)
	}
}
