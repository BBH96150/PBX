package store

import (
	"context"

	"github.com/google/uuid"
)

// DomainTenant maps a SIP domain to its owning tenant. Used by the global ops
// view to attribute FreeSWITCH's (tenant-agnostic) live state back to tenants.
type DomainTenant struct {
	Domain     string
	TenantID   uuid.UUID
	TenantName string
}

// ListSIPDomainTenants returns every SIP domain joined to its tenant name.
func (s *Store) ListSIPDomainTenants(ctx context.Context) ([]DomainTenant, error) {
	const q = `
		SELECT d.domain, t.id, t.name
		  FROM sip_domains d
		  JOIN tenants t ON t.id = d.tenant_id`
	rows, err := s.DB.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DomainTenant
	for rows.Next() {
		var d DomainTenant
		if err := rows.Scan(&d.Domain, &d.TenantID, &d.TenantName); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
