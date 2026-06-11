package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// DispositionCode is one of a tenant's call-outcome labels (see migration 0042).
// An admin/agent assigns ONE code to a completed call from the CDR list — this is
// post-call tagging only and does not affect routing. Color is an optional hex
// like "#22aa55" used to render a small badge; SortOrder/Active let a tenant
// curate and retire codes without deleting historical assignments.
type DispositionCode struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	Label     string    `json:"label"`
	Color     string    `json:"color,omitempty"`
	SortOrder int       `json:"sort_order"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateDispositionCodeInput struct {
	TenantID  uuid.UUID
	Label     string
	Color     string
	SortOrder int
}

func (s *Store) CreateDispositionCode(ctx context.Context, in CreateDispositionCodeInput) (*DispositionCode, error) {
	const q = `
		INSERT INTO disposition_codes (tenant_id, label, color, sort_order)
		VALUES ($1, $2, NULLIF($3,''), $4)
		RETURNING id, tenant_id, label, COALESCE(color,''), sort_order, active, created_at, updated_at`
	var d DispositionCode
	err := s.DB.QueryRow(ctx, q, in.TenantID, in.Label, in.Color, in.SortOrder).Scan(
		&d.ID, &d.TenantID, &d.Label, &d.Color, &d.SortOrder, &d.Active, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ListDispositionCodesForTenant returns all of a tenant's codes (active and
// retired), ordered for display. Used by the management page.
func (s *Store) ListDispositionCodesForTenant(ctx context.Context, tenantID uuid.UUID) ([]DispositionCode, error) {
	const q = `
		SELECT id, tenant_id, label, COALESCE(color,''), sort_order, active, created_at, updated_at
		  FROM disposition_codes WHERE tenant_id = $1
		 ORDER BY sort_order, label`
	return s.scanDispositionCodes(ctx, q, tenantID)
}

// ListActiveDispositionCodesForTenant returns only active codes, ordered for
// display — the set offered in the CDR-list assignment dropdown.
func (s *Store) ListActiveDispositionCodesForTenant(ctx context.Context, tenantID uuid.UUID) ([]DispositionCode, error) {
	const q = `
		SELECT id, tenant_id, label, COALESCE(color,''), sort_order, active, created_at, updated_at
		  FROM disposition_codes WHERE tenant_id = $1 AND active = true
		 ORDER BY sort_order, label`
	return s.scanDispositionCodes(ctx, q, tenantID)
}

func (s *Store) scanDispositionCodes(ctx context.Context, q string, tenantID uuid.UUID) ([]DispositionCode, error) {
	rows, err := s.DB.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DispositionCode
	for rows.Next() {
		var d DispositionCode
		if err := rows.Scan(
			&d.ID, &d.TenantID, &d.Label, &d.Color, &d.SortOrder, &d.Active, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SetDispositionCodeActive activates or retires a code, tenant-scoped. A retired
// code stays in the table so historical CDR assignments keep their label.
func (s *Store) SetDispositionCodeActive(ctx context.Context, tenantID, id uuid.UUID, active bool) error {
	const q = `UPDATE disposition_codes SET active = $3, updated_at = now() WHERE id = $1 AND tenant_id = $2`
	ct, err := s.DB.Exec(ctx, q, id, tenantID, active)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrCrossTenant
	}
	return nil
}

// DeleteDispositionCodeForTenant removes a code, tenant-scoped. Any CDRs assigned
// to it have their disposition_code_id nulled by the ON DELETE SET NULL FK.
func (s *Store) DeleteDispositionCodeForTenant(ctx context.Context, tenantID, id uuid.UUID) error {
	const q = `DELETE FROM disposition_codes WHERE id = $1 AND tenant_id = $2`
	ct, err := s.DB.Exec(ctx, q, id, tenantID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrCrossTenant
	}
	return nil
}
