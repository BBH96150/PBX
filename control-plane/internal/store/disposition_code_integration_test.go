//go:build integration

package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestIntegrationDispositionCodeCRUD(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)

	sale, err := s.CreateDispositionCode(ctx, CreateDispositionCodeInput{
		TenantID: ten.ID, Label: "Sale", Color: "#22aa55", SortOrder: 1,
	})
	if err != nil {
		t.Fatalf("CreateDispositionCode: %v", err)
	}
	if sale.Label != "Sale" || sale.Color != "#22aa55" || sale.SortOrder != 1 || !sale.Active {
		t.Fatalf("unexpected code: %+v", sale)
	}

	// A code with no color scans NULL→"" safely.
	spam, err := s.CreateDispositionCode(ctx, CreateDispositionCodeInput{
		TenantID: ten.ID, Label: "Spam",
	})
	if err != nil {
		t.Fatalf("CreateDispositionCode (no color): %v", err)
	}
	if spam.Color != "" {
		t.Fatalf("expected empty color, got %q", spam.Color)
	}

	// UNIQUE(tenant_id, label) is enforced.
	if _, err := s.CreateDispositionCode(ctx, CreateDispositionCodeInput{TenantID: ten.ID, Label: "Sale"}); err == nil {
		t.Fatal("duplicate label should violate UNIQUE(tenant_id, label)")
	}

	// List shows both.
	all, err := s.ListDispositionCodesForTenant(ctx, ten.ID)
	if err != nil || len(all) != 2 {
		t.Fatalf("ListDispositionCodes: %d codes, err %v", len(all), err)
	}

	// Retire "Spam" → still in full list, absent from active list.
	if err := s.SetDispositionCodeActive(ctx, ten.ID, spam.ID, false); err != nil {
		t.Fatalf("SetDispositionCodeActive: %v", err)
	}
	active, err := s.ListActiveDispositionCodesForTenant(ctx, ten.ID)
	if err != nil || len(active) != 1 || active[0].ID != sale.ID {
		t.Fatalf("active list: %d codes (want 1=Sale), err %v", len(active), err)
	}
	if full, _ := s.ListDispositionCodesForTenant(ctx, ten.ID); len(full) != 2 {
		t.Fatalf("full list should still show retired code, got %d", len(full))
	}

	// Delete → gone.
	if err := s.DeleteDispositionCodeForTenant(ctx, ten.ID, spam.ID); err != nil {
		t.Fatalf("DeleteDispositionCode: %v", err)
	}
	if after, _ := s.ListDispositionCodesForTenant(ctx, ten.ID); len(after) != 1 {
		t.Fatalf("expected 1 after delete, got %d", len(after))
	}
}

func TestIntegrationDispositionCodeCrossTenantGuard(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tenA := makeTenant(t, s)
	tenB := makeTenant(t, s)

	codeA, err := s.CreateDispositionCode(ctx, CreateDispositionCodeInput{TenantID: tenA.ID, Label: "Sale"})
	if err != nil {
		t.Fatalf("CreateDispositionCode: %v", err)
	}

	// Tenant B cannot retire or delete A's code.
	if err := s.SetDispositionCodeActive(ctx, tenB.ID, codeA.ID, false); !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("cross-tenant retire: want ErrCrossTenant, got %v", err)
	}
	if err := s.DeleteDispositionCodeForTenant(ctx, tenB.ID, codeA.ID); !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("cross-tenant delete: want ErrCrossTenant, got %v", err)
	}
	// B's active list never sees A's code.
	if act, _ := s.ListActiveDispositionCodesForTenant(ctx, tenB.ID); len(act) != 0 {
		t.Fatalf("tenant B should see no codes, got %d", len(act))
	}
}

// makeCDR inserts a minimal CDR for the given tenant and returns its id.
func makeCDR(t *testing.T, s *Store, ten *Tenant) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	callUUID := "disp-" + uuid.NewString()
	cdr := &CDR{
		TenantID:    &ten.ID,
		CallUUID:    callUUID,
		Direction:   "inbound",
		FromURI:     "sip:caller@example.com",
		ToURI:       "sip:1001@pbx.local",
		StartedAt:   time.Now().UTC(),
		HangupCause: "NORMAL_CLEARING",
	}
	if err := s.CreateCDR(ctx, cdr); err != nil {
		t.Fatalf("CreateCDR: %v", err)
	}
	var id uuid.UUID
	if err := s.DB.QueryRow(ctx, `SELECT id FROM cdrs WHERE call_uuid=$1`, callUUID).Scan(&id); err != nil {
		t.Fatalf("resolve cdr id: %v", err)
	}
	return id
}

func TestIntegrationCDRDispositionAssignClear(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)

	code, err := s.CreateDispositionCode(ctx, CreateDispositionCodeInput{
		TenantID: ten.ID, Label: "Sale", Color: "#22aa55",
	})
	if err != nil {
		t.Fatalf("CreateDispositionCode: %v", err)
	}
	cdrID := makeCDR(t, s, ten)

	// Assign.
	if err := s.SetCDRDisposition(ctx, ten.ID, cdrID, &code.ID); err != nil {
		t.Fatalf("SetCDRDisposition assign: %v", err)
	}
	got, err := s.GetCDRForTenant(ctx, ten.ID, cdrID)
	if err != nil {
		t.Fatalf("GetCDRForTenant: %v", err)
	}
	if got.DispositionCodeID == nil || *got.DispositionCodeID != code.ID {
		t.Fatalf("expected disposition_code_id=%s, got %+v", code.ID, got.DispositionCodeID)
	}
	if got.DispositionLabel != "Sale" || got.DispositionColor != "#22aa55" {
		t.Fatalf("expected joined label/color, got %q/%q", got.DispositionLabel, got.DispositionColor)
	}
	// And it surfaces through the list query too.
	list, _ := s.ListCDRsFilteredForTenant(ctx, ten.ID, CDRFilter{})
	if len(list) == 0 || list[0].DispositionLabel != "Sale" {
		t.Fatalf("list query should join the disposition label, got %+v", list)
	}

	// Clear (nil).
	if err := s.SetCDRDisposition(ctx, ten.ID, cdrID, nil); err != nil {
		t.Fatalf("SetCDRDisposition clear: %v", err)
	}
	got, _ = s.GetCDRForTenant(ctx, ten.ID, cdrID)
	if got.DispositionCodeID != nil || got.DispositionLabel != "" {
		t.Fatalf("expected cleared disposition, got %+v / %q", got.DispositionCodeID, got.DispositionLabel)
	}

	// Deleting an assigned code nulls the CDR's reference (ON DELETE SET NULL).
	if err := s.SetCDRDisposition(ctx, ten.ID, cdrID, &code.ID); err != nil {
		t.Fatalf("re-assign: %v", err)
	}
	if err := s.DeleteDispositionCodeForTenant(ctx, ten.ID, code.ID); err != nil {
		t.Fatalf("DeleteDispositionCode: %v", err)
	}
	got, _ = s.GetCDRForTenant(ctx, ten.ID, cdrID)
	if got.DispositionCodeID != nil {
		t.Fatalf("ON DELETE SET NULL should have cleared the CDR, got %+v", got.DispositionCodeID)
	}
}

func TestIntegrationCDRDispositionCrossTenantRejected(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tenA := makeTenant(t, s)
	tenB := makeTenant(t, s)

	codeB, err := s.CreateDispositionCode(ctx, CreateDispositionCodeInput{TenantID: tenB.ID, Label: "Spam"})
	if err != nil {
		t.Fatalf("CreateDispositionCode (B): %v", err)
	}
	cdrA := makeCDR(t, s, tenA)

	// Assigning tenant B's code to tenant A's CDR must NOT take effect. The
	// subquery yields NULL (code not in tenant A), so the column stays NULL while
	// the CDR row itself belongs to A (RowsAffected == 1, no error).
	if err := s.SetCDRDisposition(ctx, tenA.ID, cdrA, &codeB.ID); err != nil {
		t.Fatalf("SetCDRDisposition (cross-tenant code): unexpected err %v", err)
	}
	got, _ := s.GetCDRForTenant(ctx, tenA.ID, cdrA)
	if got.DispositionCodeID != nil {
		t.Fatalf("cross-tenant code must not be linked, got %+v", got.DispositionCodeID)
	}

	// Assigning to a CDR that isn't tenant A's (here tenant B operating on A's
	// CDR) is rejected with ErrCrossTenant (RowsAffected == 0).
	codeA, _ := s.CreateDispositionCode(ctx, CreateDispositionCodeInput{TenantID: tenA.ID, Label: "Sale"})
	if err := s.SetCDRDisposition(ctx, tenB.ID, cdrA, &codeA.ID); !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("cross-tenant CDR: want ErrCrossTenant, got %v", err)
	}
}
