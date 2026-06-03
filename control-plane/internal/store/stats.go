package store

import (
	"context"

	"github.com/google/uuid"
)

// PlatformStats are the headline counts shown on the dashboard. When tenantID
// is non-nil the counts are scoped to that tenant (for tenant-scoped tokens).
type PlatformStats struct {
	Tenants    int
	Extensions int
	DIDs       int
	CallsToday int
}

func (s *Store) GetPlatformStats(ctx context.Context, tenantID *uuid.UUID) (PlatformStats, error) {
	var st PlatformStats
	if tenantID == nil {
		if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM tenants`).Scan(&st.Tenants); err != nil {
			return st, err
		}
	} else {
		st.Tenants = 1
	}
	// One round-trip for the three tenant-scoped counts.
	const q = `
		SELECT
		  (SELECT count(*) FROM extensions WHERE status='active' AND ($1::uuid IS NULL OR tenant_id=$1)),
		  (SELECT count(*) FROM dids       WHERE ($1::uuid IS NULL OR tenant_id=$1)),
		  (SELECT count(*) FROM cdrs       WHERE started_at >= date_trunc('day', now()) AND ($1::uuid IS NULL OR tenant_id=$1))`
	if err := s.DB.QueryRow(ctx, q, tenantID).Scan(&st.Extensions, &st.DIDs, &st.CallsToday); err != nil {
		return st, err
	}
	return st, nil
}

// TenantCounts holds per-tenant entity counts for the dashboard workspace table.
type TenantCounts struct {
	Extensions int
	DIDs       int
}

// GetTenantCounts returns extension/DID counts keyed by tenant ID for every
// tenant, in a single query (dashboard workspace table).
func (s *Store) GetTenantCounts(ctx context.Context) (map[uuid.UUID]TenantCounts, error) {
	const q = `
		SELECT t.id,
		       (SELECT count(*) FROM extensions e WHERE e.tenant_id = t.id AND e.status='active'),
		       (SELECT count(*) FROM dids d WHERE d.tenant_id = t.id)
		  FROM tenants t`
	rows, err := s.DB.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[uuid.UUID]TenantCounts)
	for rows.Next() {
		var id uuid.UUID
		var c TenantCounts
		if err := rows.Scan(&id, &c.Extensions, &c.DIDs); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}
