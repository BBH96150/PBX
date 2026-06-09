package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// E911Location is a dispatchable civic address (RAY BAUM's Act). A tenant admin
// defines named locations; each extension may be assigned one (see migration
// 0038). When a phone dials 911 the dialplan resolves the calling extension's
// location and stamps it onto the outbound emergency call.
type E911Location struct {
	ID         uuid.UUID `json:"id"`
	TenantID   uuid.UUID `json:"tenant_id"`
	Label      string    `json:"label"`
	Street     string    `json:"street"`
	Street2    string    `json:"street2,omitempty"`
	City       string    `json:"city"`
	Region     string    `json:"region"`
	PostalCode string    `json:"postal_code"`
	Country    string    `json:"country"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// SingleLine renders the address as one comma-separated line for stamping onto
// the emergency call (channel var / webhook payload).
func (l *E911Location) SingleLine() string {
	parts := []string{l.Street}
	if l.Street2 != "" {
		parts = append(parts, l.Street2)
	}
	parts = append(parts, l.City, l.Region, l.PostalCode)
	if l.Country != "" {
		parts = append(parts, l.Country)
	}
	out := ""
	for i, p := range parts {
		if p == "" {
			continue
		}
		if i > 0 && out != "" {
			out += ", "
		}
		out += p
	}
	return out
}

type CreateE911LocationInput struct {
	TenantID   uuid.UUID
	Label      string
	Street     string
	Street2    string
	City       string
	Region     string
	PostalCode string
	Country    string
}

func (s *Store) CreateE911Location(ctx context.Context, in CreateE911LocationInput) (*E911Location, error) {
	country := in.Country
	if country == "" {
		country = "US"
	}
	const q = `
		INSERT INTO e911_locations
		    (tenant_id, label, street, street2, city, region, postal_code, country)
		VALUES ($1, $2, $3, NULLIF($4,''), $5, $6, $7, $8)
		RETURNING id, tenant_id, label, street, COALESCE(street2,''),
		          city, region, postal_code, country, enabled, created_at, updated_at`
	var l E911Location
	err := s.DB.QueryRow(ctx, q,
		in.TenantID, in.Label, in.Street, in.Street2, in.City, in.Region, in.PostalCode, country,
	).Scan(
		&l.ID, &l.TenantID, &l.Label, &l.Street, &l.Street2,
		&l.City, &l.Region, &l.PostalCode, &l.Country, &l.Enabled, &l.CreatedAt, &l.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (s *Store) ListE911LocationsForTenant(ctx context.Context, tenantID uuid.UUID) ([]E911Location, error) {
	const q = `
		SELECT id, tenant_id, label, street, COALESCE(street2,''),
		       city, region, postal_code, country, enabled, created_at, updated_at
		  FROM e911_locations WHERE tenant_id = $1
		 ORDER BY label`
	rows, err := s.DB.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []E911Location
	for rows.Next() {
		var l E911Location
		if err := rows.Scan(
			&l.ID, &l.TenantID, &l.Label, &l.Street, &l.Street2,
			&l.City, &l.Region, &l.PostalCode, &l.Country, &l.Enabled, &l.CreatedAt, &l.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// GetE911LocationForTenant fetches one location, enforcing tenant ownership.
// Returns pgx.ErrNoRows if not found for that tenant.
func (s *Store) GetE911LocationForTenant(ctx context.Context, tenantID, id uuid.UUID) (*E911Location, error) {
	const q = `
		SELECT id, tenant_id, label, street, COALESCE(street2,''),
		       city, region, postal_code, country, enabled, created_at, updated_at
		  FROM e911_locations WHERE id = $1 AND tenant_id = $2`
	var l E911Location
	err := s.DB.QueryRow(ctx, q, id, tenantID).Scan(
		&l.ID, &l.TenantID, &l.Label, &l.Street, &l.Street2,
		&l.City, &l.Region, &l.PostalCode, &l.Country, &l.Enabled, &l.CreatedAt, &l.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// DeleteE911LocationForTenant removes a location, tenant-scoped. Extensions
// pointing at it are detached via ON DELETE SET NULL.
func (s *Store) DeleteE911LocationForTenant(ctx context.Context, tenantID, id uuid.UUID) error {
	const q = `DELETE FROM e911_locations WHERE id = $1 AND tenant_id = $2`
	ct, err := s.DB.Exec(ctx, q, id, tenantID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrCrossTenant
	}
	return nil
}

// SetE911LocationEnabled flips a location's enabled flag, tenant-scoped.
func (s *Store) SetE911LocationEnabled(ctx context.Context, tenantID, id uuid.UUID, enabled bool) error {
	const q = `UPDATE e911_locations SET enabled = $3, updated_at = now()
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

// SetExtensionE911Location assigns (or clears, when locID is nil) an extension's
// dispatchable location, tenant-scoped. A non-nil locID must also belong to the
// tenant — enforced via the subquery so a cross-tenant location can't be linked.
func (s *Store) SetExtensionE911Location(ctx context.Context, tenantID, extID uuid.UUID, locID *uuid.UUID) error {
	const q = `
		UPDATE extensions
		   SET e911_location_id = (
		         SELECT id FROM e911_locations
		          WHERE id = $3 AND tenant_id = $2
		       ),
		       updated_at = now()
		 WHERE id = $1 AND tenant_id = $2`
	ct, err := s.DB.Exec(ctx, q, extID, tenantID, locID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrCrossTenant
	}
	return nil
}

// E911Resolution is the dialplan's view of who is dialing 911: the calling
// extension's identity plus its dispatchable address (Address is nil when the
// extension has no location assigned).
type E911Resolution struct {
	TenantID    uuid.UUID
	SIPUsername string
	Extension   string
	Address     *E911Location
}

// ResolveE911ForExtensionNumber resolves (tenant_domain, extension_number) →
// the calling extension's tenant_id + sip_username + assigned dispatchable
// address (Address is nil if the extension has no e911_location). This is a
// dedicated, narrow query used ONLY by the dialplan emergency handler — it does
// NOT touch the main Extension scan paths. Returns pgx.ErrNoRows if no such
// extension exists in the domain.
func (s *Store) ResolveE911ForExtensionNumber(ctx context.Context, tenantDomain, extNumber string) (*E911Resolution, error) {
	// COALESCE every joined location column: on an unassigned extension the
	// LEFT JOIN yields all-NULL location columns, which can't scan into the
	// non-nullable struct fields. `lid` (the only genuinely-nullable scan)
	// tells us whether a real address was joined.
	const q = `
		SELECT e.tenant_id, e.sip_username, e.extension,
		       l.id,
		       COALESCE(l.tenant_id, '00000000-0000-0000-0000-000000000000'::uuid),
		       COALESCE(l.label,''), COALESCE(l.street,''), COALESCE(l.street2,''),
		       COALESCE(l.city,''), COALESCE(l.region,''), COALESCE(l.postal_code,''),
		       COALESCE(l.country,''), COALESCE(l.enabled,false),
		       COALESCE(l.created_at, now()), COALESCE(l.updated_at, now())
		  FROM extensions e
		  JOIN sip_domains d ON d.id = e.sip_domain_id
		  LEFT JOIN e911_locations l ON l.id = e.e911_location_id
		 WHERE d.domain = $1 AND e.extension = $2 AND e.status = 'active'
		 LIMIT 1`
	var (
		res E911Resolution
		lid *uuid.UUID
		l   E911Location
	)
	err := s.DB.QueryRow(ctx, q, tenantDomain, extNumber).Scan(
		&res.TenantID, &res.SIPUsername, &res.Extension,
		&lid, &l.TenantID, &l.Label, &l.Street, &l.Street2,
		&l.City, &l.Region, &l.PostalCode, &l.Country, &l.Enabled,
		&l.CreatedAt, &l.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if lid != nil {
		l.ID = *lid
		res.Address = &l
	}
	return &res, nil
}
