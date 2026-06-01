package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrDIDNotFound is returned when a tenant-scoped DID delete/update misses.
var ErrDIDNotFound = errors.New("DID not found for this tenant")

type DID struct {
	ID               uuid.UUID  `json:"id"`
	TenantID         uuid.UUID  `json:"tenant_id"`
	CarrierID        uuid.UUID  `json:"carrier_id"`
	CarrierAccountID *uuid.UUID `json:"carrier_account_id,omitempty"`
	E164             string     `json:"e164"`
	DestinationKind  string     `json:"destination_kind"`
	DestinationID    uuid.UUID  `json:"destination_id"`
	CNAM             string     `json:"cnam,omitempty"`
	Enabled          bool       `json:"enabled"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type CreateDIDInput struct {
	TenantID         uuid.UUID
	CarrierID        uuid.UUID
	CarrierAccountID *uuid.UUID
	E164             string
	DestinationKind  string
	DestinationID    uuid.UUID
	CNAM             string
}

func (s *Store) CreateDID(ctx context.Context, in CreateDIDInput) (*DID, error) {
	const q = `
		INSERT INTO dids
		    (tenant_id, carrier_id, carrier_account_id, e164,
		     destination_kind, destination_id, cnam)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7,''))
		RETURNING id, tenant_id, carrier_id, carrier_account_id, e164,
		          destination_kind, destination_id, COALESCE(cnam,''),
		          enabled, created_at, updated_at`
	var d DID
	err := s.DB.QueryRow(ctx, q,
		in.TenantID, in.CarrierID, in.CarrierAccountID, in.E164,
		in.DestinationKind, in.DestinationID, in.CNAM,
	).Scan(
		&d.ID, &d.TenantID, &d.CarrierID, &d.CarrierAccountID, &d.E164,
		&d.DestinationKind, &d.DestinationID, &d.CNAM,
		&d.Enabled, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) ListDIDsForTenant(ctx context.Context, tenantID uuid.UUID) ([]DID, error) {
	const q = `
		SELECT id, tenant_id, carrier_id, carrier_account_id, e164,
		       destination_kind, destination_id, COALESCE(cnam,''),
		       enabled, created_at, updated_at
		  FROM dids WHERE tenant_id = $1
		 ORDER BY e164`
	rows, err := s.DB.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DID
	for rows.Next() {
		var d DID
		if err := rows.Scan(
			&d.ID, &d.TenantID, &d.CarrierID, &d.CarrierAccountID, &d.E164,
			&d.DestinationKind, &d.DestinationID, &d.CNAM,
			&d.Enabled, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// UpdateDIDDestinationInput re-routes an existing DID to a new destination
// and/or updates its CNAM. Trunk/carrier binding is left unchanged.
type UpdateDIDDestinationInput struct {
	DestinationKind string
	DestinationID   uuid.UUID
	CNAM            string
}

// UpdateDIDDestinationForTenant changes where an inbound DID routes. Returns
// ErrDIDNotFound if the row doesn't exist or belongs to another tenant.
func (s *Store) UpdateDIDDestinationForTenant(ctx context.Context, tenantID, id uuid.UUID, in UpdateDIDDestinationInput) error {
	tag, err := s.DB.Exec(ctx, `
		UPDATE dids
		   SET destination_kind = $3, destination_id = $4,
		       cnam = NULLIF($5,''), updated_at = now()
		 WHERE id = $1 AND tenant_id = $2`,
		id, tenantID, in.DestinationKind, in.DestinationID, in.CNAM)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrDIDNotFound
	}
	return nil
}

// DeleteDIDForTenant removes a DID from a tenant. Returns an error if the
// row doesn't exist or belongs to a different tenant (defense in depth).
func (s *Store) DeleteDIDForTenant(ctx context.Context, tenantID, id uuid.UUID) error {
	tag, err := s.DB.Exec(ctx,
		`DELETE FROM dids WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrDIDNotFound
	}
	return nil
}

// SetDIDEnabledForTenant flips the enabled flag.
func (s *Store) SetDIDEnabledForTenant(ctx context.Context, tenantID, id uuid.UUID, enabled bool) error {
	tag, err := s.DB.Exec(ctx,
		`UPDATE dids SET enabled = $3, updated_at = now() WHERE id = $1 AND tenant_id = $2`,
		id, tenantID, enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrDIDNotFound
	}
	return nil
}

// DIDExtensionTarget is what the inbound PSTN dialplan handler needs to
// build the bridge URI when destination_kind = 'extension'.
type DIDExtensionTarget struct {
	DIDID       uuid.UUID
	TenantID    uuid.UUID
	Extension   string
	SIPUsername string
	SIPDomain   string
	DisplayName string
	CNAM        string
}

// LookupDIDExtensionTarget resolves an inbound DID (in E.164 form with
// leading +) to its bound extension, only for destination_kind = 'extension'.
// Other destination kinds (ivr, queue, hunt_group) return ErrUnsupportedDest
// for now — Phase 3 plumbs those.
func (s *Store) LookupDIDExtensionTarget(ctx context.Context, e164 string) (*DIDExtensionTarget, error) {
	const q = `
		SELECT d.id, d.tenant_id, d.destination_kind,
		       e.extension, e.sip_username, sd.domain,
		       COALESCE(e.display_name,''), COALESCE(d.cnam,'')
		  FROM dids d
		  JOIN extensions e   ON e.id = d.destination_id AND d.destination_kind = 'extension'
		  JOIN sip_domains sd ON sd.id = e.sip_domain_id
		 WHERE d.e164 = $1 AND d.enabled = true AND e.status = 'active'
		 LIMIT 1`
	var t DIDExtensionTarget
	var kind string
	err := s.DB.QueryRow(ctx, q, e164).Scan(
		&t.DIDID, &t.TenantID, &kind,
		&t.Extension, &t.SIPUsername, &t.SIPDomain,
		&t.DisplayName, &t.CNAM,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
