package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// PagingGroup is a PTT/intercom target: a named set of member extensions
// reachable as a page. Mode selects the talk-path delivery (see migration
// 0035): "fs_conference" (dial extension → members auto-answer into a one-way
// page conference), "multicast" (desk phones listen on multicast_addr:port via
// ZTP), or "native" (channel backing the native softphone's hold-to-talk).
type PagingGroup struct {
	ID            uuid.UUID `json:"id"`
	TenantID      uuid.UUID `json:"tenant_id"`
	Extension     string    `json:"extension,omitempty"`
	Name          string    `json:"name"`
	Mode          string    `json:"mode"`
	MulticastAddr string    `json:"multicast_addr,omitempty"`
	MulticastPort int       `json:"multicast_port,omitempty"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	// MemberCount is populated by list queries for display; not stored.
	MemberCount int `json:"member_count,omitempty"`
}

type PagingGroupMember struct {
	ID            uuid.UUID `json:"id"`
	PagingGroupID uuid.UUID `json:"paging_group_id"`
	ExtensionID   uuid.UUID `json:"extension_id"`
	// Joined fields populated by routing/detail queries:
	SIPUsername string `json:"sip_username,omitempty"`
	SIPDomain   string `json:"sip_domain,omitempty"`
	Extension   string `json:"extension,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

type CreatePagingGroupInput struct {
	TenantID      uuid.UUID
	Extension     string
	Name          string
	Mode          string // default "fs_conference" if blank
	MulticastAddr string
	MulticastPort int
}

func (s *Store) CreatePagingGroup(ctx context.Context, in CreatePagingGroupInput) (*PagingGroup, error) {
	if in.Mode == "" {
		in.Mode = "fs_conference"
	}
	const q = `
		INSERT INTO paging_groups
		    (tenant_id, extension, name, mode, multicast_addr, multicast_port)
		VALUES ($1, NULLIF($2,''), $3, $4, NULLIF($5,''), NULLIF($6,0))
		RETURNING id, tenant_id, COALESCE(extension,''), name, mode,
		          COALESCE(multicast_addr,''), COALESCE(multicast_port,0),
		          enabled, created_at, updated_at`
	var pg PagingGroup
	err := s.DB.QueryRow(ctx, q,
		in.TenantID, in.Extension, in.Name, in.Mode, in.MulticastAddr, in.MulticastPort,
	).Scan(
		&pg.ID, &pg.TenantID, &pg.Extension, &pg.Name, &pg.Mode,
		&pg.MulticastAddr, &pg.MulticastPort, &pg.Enabled, &pg.CreatedAt, &pg.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &pg, nil
}

func (s *Store) ListPagingGroupsForTenant(ctx context.Context, tenantID uuid.UUID) ([]PagingGroup, error) {
	const q = `
		SELECT pg.id, pg.tenant_id, COALESCE(pg.extension,''), pg.name, pg.mode,
		       COALESCE(pg.multicast_addr,''), COALESCE(pg.multicast_port,0),
		       pg.enabled, pg.created_at, pg.updated_at,
		       (SELECT count(*) FROM paging_group_members m WHERE m.paging_group_id = pg.id)
		  FROM paging_groups pg WHERE pg.tenant_id = $1
		 ORDER BY pg.name`
	rows, err := s.DB.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PagingGroup
	for rows.Next() {
		var pg PagingGroup
		if err := rows.Scan(
			&pg.ID, &pg.TenantID, &pg.Extension, &pg.Name, &pg.Mode,
			&pg.MulticastAddr, &pg.MulticastPort, &pg.Enabled,
			&pg.CreatedAt, &pg.UpdatedAt, &pg.MemberCount,
		); err != nil {
			return nil, err
		}
		out = append(out, pg)
	}
	return out, rows.Err()
}

// GetPagingGroupForTenant fetches one group, enforcing tenant ownership.
// Returns pgx.ErrNoRows if not found for that tenant.
func (s *Store) GetPagingGroupForTenant(ctx context.Context, tenantID, id uuid.UUID) (*PagingGroup, error) {
	const q = `
		SELECT id, tenant_id, COALESCE(extension,''), name, mode,
		       COALESCE(multicast_addr,''), COALESCE(multicast_port,0),
		       enabled, created_at, updated_at
		  FROM paging_groups WHERE id = $1 AND tenant_id = $2`
	var pg PagingGroup
	err := s.DB.QueryRow(ctx, q, id, tenantID).Scan(
		&pg.ID, &pg.TenantID, &pg.Extension, &pg.Name, &pg.Mode,
		&pg.MulticastAddr, &pg.MulticastPort, &pg.Enabled, &pg.CreatedAt, &pg.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &pg, nil
}

// DeletePagingGroupForTenant removes a group (members cascade). Tenant-scoped.
func (s *Store) DeletePagingGroupForTenant(ctx context.Context, tenantID, id uuid.UUID) error {
	const q = `DELETE FROM paging_groups WHERE id = $1 AND tenant_id = $2`
	ct, err := s.DB.Exec(ctx, q, id, tenantID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrCrossTenant
	}
	return nil
}

// SetPagingGroupEnabled flips a group's enabled flag, tenant-scoped.
func (s *Store) SetPagingGroupEnabled(ctx context.Context, tenantID, id uuid.UUID, enabled bool) error {
	const q = `UPDATE paging_groups SET enabled = $3, updated_at = now()
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

// AddPagingMember adds an extension to a group. The extension must belong to
// the same tenant as the group (else ErrCrossTenant). Idempotent on the
// (group, extension) unique constraint — a duplicate add returns the existing
// row's id via ON CONFLICT.
func (s *Store) AddPagingMember(ctx context.Context, groupID, extensionID uuid.UUID) (*PagingGroupMember, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const checkQ = `
		SELECT pg.tenant_id = e.tenant_id
		  FROM paging_groups pg, extensions e
		 WHERE pg.id = $1 AND e.id = $2`
	var sameTenant bool
	if err := tx.QueryRow(ctx, checkQ, groupID, extensionID).Scan(&sameTenant); err != nil {
		return nil, err
	}
	if !sameTenant {
		return nil, ErrCrossTenant
	}

	const ins = `
		INSERT INTO paging_group_members (paging_group_id, extension_id)
		VALUES ($1, $2)
		ON CONFLICT (paging_group_id, extension_id)
		    DO UPDATE SET paging_group_id = EXCLUDED.paging_group_id
		RETURNING id, paging_group_id, extension_id`
	var m PagingGroupMember
	if err := tx.QueryRow(ctx, ins, groupID, extensionID).Scan(
		&m.ID, &m.PagingGroupID, &m.ExtensionID,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &m, nil
}

// RemovePagingMemberForTenant deletes a membership, scoped to the tenant so a
// caller can't remove members from another tenant's group.
func (s *Store) RemovePagingMemberForTenant(ctx context.Context, tenantID, memberID uuid.UUID) error {
	const q = `
		DELETE FROM paging_group_members m
		 USING paging_groups pg
		 WHERE m.id = $1 AND m.paging_group_id = pg.id AND pg.tenant_id = $2`
	ct, err := s.DB.Exec(ctx, q, memberID, tenantID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrCrossTenant
	}
	return nil
}

// ListPagingMembersDetailed returns a group's members joined with their
// extension number / display name, for portal display and dialplan generation.
func (s *Store) ListPagingMembersDetailed(ctx context.Context, groupID uuid.UUID) ([]PagingGroupMember, error) {
	const q = `
		SELECT m.id, m.paging_group_id, m.extension_id,
		       e.sip_username, sd.domain, e.extension, COALESCE(e.display_name,'')
		  FROM paging_group_members m
		  JOIN extensions  e  ON e.id  = m.extension_id
		  JOIN sip_domains sd ON sd.id = e.sip_domain_id
		 WHERE m.paging_group_id = $1
		 ORDER BY e.extension`
	rows, err := s.DB.Query(ctx, q, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PagingGroupMember
	for rows.Next() {
		var m PagingGroupMember
		if err := rows.Scan(
			&m.ID, &m.PagingGroupID, &m.ExtensionID,
			&m.SIPUsername, &m.SIPDomain, &m.Extension, &m.DisplayName,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListMulticastPagingForExtensions returns the enabled multicast paging groups
// (with an address configured) that any of the given extensions belong to,
// deduped and ordered by name. Used by device provisioning to tell a phone
// which multicast addresses to listen on. Returns nil for an empty input.
func (s *Store) ListMulticastPagingForExtensions(ctx context.Context, extIDs []uuid.UUID) ([]PagingGroup, error) {
	if len(extIDs) == 0 {
		return nil, nil
	}
	const q = `
		SELECT DISTINCT pg.id, pg.tenant_id, COALESCE(pg.extension,''), pg.name,
		       pg.mode, COALESCE(pg.multicast_addr,''), COALESCE(pg.multicast_port,0),
		       pg.enabled, pg.created_at, pg.updated_at
		  FROM paging_groups pg
		  JOIN paging_group_members m ON m.paging_group_id = pg.id
		 WHERE pg.mode = 'multicast'
		   AND pg.enabled = true
		   AND pg.multicast_addr IS NOT NULL
		   AND m.extension_id = ANY($1)
		 ORDER BY pg.name`
	rows, err := s.DB.Query(ctx, q, extIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PagingGroup
	for rows.Next() {
		var pg PagingGroup
		if err := rows.Scan(
			&pg.ID, &pg.TenantID, &pg.Extension, &pg.Name, &pg.Mode,
			&pg.MulticastAddr, &pg.MulticastPort, &pg.Enabled, &pg.CreatedAt, &pg.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, pg)
	}
	return out, rows.Err()
}

// PagingRoutingInfo is everything the dialplan handler needs in one trip.
type PagingRoutingInfo struct {
	Group   PagingGroup
	Members []PagingGroupMember
}

// LookupPagingGroupByExtension resolves an internal-dialed page code (e.g.
// "800") within a tenant domain to a paging group + its members (active
// extensions only). Returns pgx.ErrNoRows if no enabled group has that
// extension. Used by the FS xml_curl dialplan handler.
func (s *Store) LookupPagingGroupByExtension(ctx context.Context, tenantDomain, ext string) (*PagingRoutingInfo, error) {
	const headerQ = `
		SELECT pg.id, pg.tenant_id, COALESCE(pg.extension,''), pg.name, pg.mode,
		       COALESCE(pg.multicast_addr,''), COALESCE(pg.multicast_port,0),
		       pg.enabled, pg.created_at, pg.updated_at
		  FROM paging_groups pg
		  JOIN sip_domains sd ON sd.tenant_id = pg.tenant_id
		 WHERE sd.domain = $1 AND pg.extension = $2 AND pg.enabled = true
		 LIMIT 1`
	var pg PagingGroup
	err := s.DB.QueryRow(ctx, headerQ, tenantDomain, ext).Scan(
		&pg.ID, &pg.TenantID, &pg.Extension, &pg.Name, &pg.Mode,
		&pg.MulticastAddr, &pg.MulticastPort, &pg.Enabled, &pg.CreatedAt, &pg.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	members, err := s.fetchPagingMembersActive(ctx, pg.ID)
	if err != nil {
		return nil, err
	}
	return &PagingRoutingInfo{Group: pg, Members: members}, nil
}

func (s *Store) fetchPagingMembersActive(ctx context.Context, groupID uuid.UUID) ([]PagingGroupMember, error) {
	const q = `
		SELECT m.id, m.paging_group_id, m.extension_id,
		       e.sip_username, sd.domain, e.extension, COALESCE(e.display_name,'')
		  FROM paging_group_members m
		  JOIN extensions  e  ON e.id  = m.extension_id
		  JOIN sip_domains sd ON sd.id = e.sip_domain_id
		 WHERE m.paging_group_id = $1
		   AND e.status = 'active'
		 ORDER BY e.extension`
	rows, err := s.DB.Query(ctx, q, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PagingGroupMember
	for rows.Next() {
		var m PagingGroupMember
		if err := rows.Scan(
			&m.ID, &m.PagingGroupID, &m.ExtensionID,
			&m.SIPUsername, &m.SIPDomain, &m.Extension, &m.DisplayName,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
