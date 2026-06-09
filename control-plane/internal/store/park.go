package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ParkLot is a call-park "orbit" lot (see migration 0037). Blind-transferring a
// call to FeatureCode parks it into a numbered slot in [SlotStart, SlotEnd];
// dialing that slot number from any phone retrieves the parked call.
type ParkLot struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	Name        string    `json:"name"`
	FeatureCode string    `json:"feature_code"`
	SlotStart   int       `json:"slot_start"`
	SlotEnd     int       `json:"slot_end"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateParkLotInput struct {
	TenantID    uuid.UUID
	Name        string
	FeatureCode string
	SlotStart   int
	SlotEnd     int
}

func (s *Store) CreateParkLot(ctx context.Context, in CreateParkLotInput) (*ParkLot, error) {
	const q = `
		INSERT INTO park_lots
		    (tenant_id, name, feature_code, slot_start, slot_end)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, tenant_id, name, feature_code, slot_start, slot_end,
		          enabled, created_at, updated_at`
	var p ParkLot
	err := s.DB.QueryRow(ctx, q,
		in.TenantID, in.Name, in.FeatureCode, in.SlotStart, in.SlotEnd,
	).Scan(
		&p.ID, &p.TenantID, &p.Name, &p.FeatureCode, &p.SlotStart, &p.SlotEnd,
		&p.Enabled, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) ListParkLotsForTenant(ctx context.Context, tenantID uuid.UUID) ([]ParkLot, error) {
	const q = `
		SELECT id, tenant_id, name, feature_code, slot_start, slot_end,
		       enabled, created_at, updated_at
		  FROM park_lots WHERE tenant_id = $1
		 ORDER BY name`
	rows, err := s.DB.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ParkLot
	for rows.Next() {
		var p ParkLot
		if err := rows.Scan(
			&p.ID, &p.TenantID, &p.Name, &p.FeatureCode, &p.SlotStart, &p.SlotEnd,
			&p.Enabled, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetParkLotForTenant fetches one lot, enforcing tenant ownership.
// Returns pgx.ErrNoRows if not found for that tenant.
func (s *Store) GetParkLotForTenant(ctx context.Context, tenantID, id uuid.UUID) (*ParkLot, error) {
	const q = `
		SELECT id, tenant_id, name, feature_code, slot_start, slot_end,
		       enabled, created_at, updated_at
		  FROM park_lots WHERE id = $1 AND tenant_id = $2`
	var p ParkLot
	err := s.DB.QueryRow(ctx, q, id, tenantID).Scan(
		&p.ID, &p.TenantID, &p.Name, &p.FeatureCode, &p.SlotStart, &p.SlotEnd,
		&p.Enabled, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// DeleteParkLotForTenant removes a lot, tenant-scoped.
func (s *Store) DeleteParkLotForTenant(ctx context.Context, tenantID, id uuid.UUID) error {
	const q = `DELETE FROM park_lots WHERE id = $1 AND tenant_id = $2`
	ct, err := s.DB.Exec(ctx, q, id, tenantID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrCrossTenant
	}
	return nil
}

// SetParkLotEnabled flips a lot's enabled flag, tenant-scoped.
func (s *Store) SetParkLotEnabled(ctx context.Context, tenantID, id uuid.UUID, enabled bool) error {
	const q = `UPDATE park_lots SET enabled = $3, updated_at = now()
	            WHERE id = $1 AND tenant_id = $2`
	ct, err := s.DB.Exec(ctx, q, id, tenantID, enabled)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrCrossTenant
	}
	return nil
}

// LookupParkLotByFeatureCode resolves an internal-dialed park feature code
// (e.g. *68) within a tenant domain to an enabled park lot. Returns
// pgx.ErrNoRows if no enabled lot has that feature code. Used by the FS
// xml_curl dialplan handler to park a call.
func (s *Store) LookupParkLotByFeatureCode(ctx context.Context, tenantDomain, code string) (*ParkLot, error) {
	const q = `
		SELECT pl.id, pl.tenant_id, pl.name, pl.feature_code,
		       pl.slot_start, pl.slot_end, pl.enabled,
		       pl.created_at, pl.updated_at
		  FROM park_lots pl
		  JOIN sip_domains sd ON sd.tenant_id = pl.tenant_id
		 WHERE sd.domain = $1 AND pl.feature_code = $2 AND pl.enabled = true
		 LIMIT 1`
	var p ParkLot
	err := s.DB.QueryRow(ctx, q, tenantDomain, code).Scan(
		&p.ID, &p.TenantID, &p.Name, &p.FeatureCode, &p.SlotStart, &p.SlotEnd,
		&p.Enabled, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// LookupParkLotBySlot resolves an internal-dialed slot number within a tenant
// domain to the enabled park lot whose range [slot_start, slot_end] contains it.
// Returns pgx.ErrNoRows if no enabled lot covers that slot. Used by the FS
// xml_curl dialplan handler to retrieve a parked call.
func (s *Store) LookupParkLotBySlot(ctx context.Context, tenantDomain string, slot int) (*ParkLot, error) {
	const q = `
		SELECT pl.id, pl.tenant_id, pl.name, pl.feature_code,
		       pl.slot_start, pl.slot_end, pl.enabled,
		       pl.created_at, pl.updated_at
		  FROM park_lots pl
		  JOIN sip_domains sd ON sd.tenant_id = pl.tenant_id
		 WHERE sd.domain = $1 AND pl.enabled = true
		   AND pl.slot_start <= $2 AND pl.slot_end >= $2
		 LIMIT 1`
	var p ParkLot
	err := s.DB.QueryRow(ctx, q, tenantDomain, slot).Scan(
		&p.ID, &p.TenantID, &p.Name, &p.FeatureCode, &p.SlotStart, &p.SlotEnd,
		&p.Enabled, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
