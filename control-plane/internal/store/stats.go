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
