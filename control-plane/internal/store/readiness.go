package store

import (
	"context"

	"github.com/google/uuid"
)

// Readiness aggregation read-queries. These are tenant-scoped (WHERE
// tenant_id=$1) COUNT/aggregation queries that back the portal-only tenant
// readiness / health dashboard. They collect no new data — they read existing
// tables (extensions, voicemail_boxes, user_2fa_methods) — and add no schema.

// E911Coverage summarizes how many of a tenant's active extensions have a
// dispatchable E911 location assigned (Kari's Law / RAY BAUM readiness).
type E911Coverage struct {
	ActiveExtensions int // active extensions in the tenant
	WithLocation     int // of those, how many have e911_location_id set
}

// E911CoverageForTenant counts active extensions and how many carry an E911
// location. Missing = ActiveExtensions - WithLocation.
func (s *Store) E911CoverageForTenant(ctx context.Context, tenantID uuid.UUID) (E911Coverage, error) {
	const q = `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE e911_location_id IS NOT NULL)
		  FROM extensions
		 WHERE tenant_id = $1 AND status = 'active'`
	var c E911Coverage
	err := s.DB.QueryRow(ctx, q, tenantID).Scan(&c.ActiveExtensions, &c.WithLocation)
	return c, err
}

// VoicemailCoverage summarizes a tenant's enabled voicemail boxes and how many
// of them lack a custom greeting (greeting_path NULL/empty).
type VoicemailCoverage struct {
	Enabled      int // enabled voicemail boxes
	WithGreeting int // of those, how many have a custom greeting recorded
}

// VoicemailCoverageForTenant counts enabled voicemail boxes and how many have a
// custom greeting path set.
func (s *Store) VoicemailCoverageForTenant(ctx context.Context, tenantID uuid.UUID) (VoicemailCoverage, error) {
	const q = `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE greeting_path IS NOT NULL AND greeting_path <> '')
		  FROM voicemail_boxes
		 WHERE tenant_id = $1 AND enabled = true`
	var c VoicemailCoverage
	err := s.DB.QueryRow(ctx, q, tenantID).Scan(&c.Enabled, &c.WithGreeting)
	return c, err
}

// Count2FAEnrolledUsersForTenant counts a tenant's active members who have at
// least one confirmed 2FA method. Informational (security posture).
func (s *Store) Count2FAEnrolledUsersForTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	const q = `
		SELECT COUNT(DISTINCT m.user_id)
		  FROM user_tenant_memberships m
		  JOIN users u ON u.id = m.user_id
		  JOIN user_2fa_methods f ON f.user_id = m.user_id
		 WHERE m.tenant_id = $1
		   AND u.status = 'active'
		   AND f.confirmed_at IS NOT NULL`
	var n int
	err := s.DB.QueryRow(ctx, q, tenantID).Scan(&n)
	return n, err
}
