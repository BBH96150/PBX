package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

type RingGroup struct {
	ID             uuid.UUID  `json:"id"`
	TenantID       uuid.UUID  `json:"tenant_id"`
	Extension      string     `json:"extension,omitempty"`
	Name           string     `json:"name"`
	Strategy       string     `json:"strategy"`
	RingTimeoutSec int        `json:"ring_timeout_sec"`
	FallbackKind   string     `json:"fallback_kind,omitempty"`
	FallbackID     *uuid.UUID `json:"fallback_id,omitempty"`
	CallerIDPrefix string     `json:"caller_id_prefix,omitempty"`
	Enabled        bool       `json:"enabled"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type RingGroupMember struct {
	ID           uuid.UUID `json:"id"`
	RingGroupID  uuid.UUID `json:"ring_group_id"`
	ExtensionID  uuid.UUID `json:"extension_id"`
	Priority     int       `json:"priority"`
	RingDelaySec int       `json:"ring_delay_sec"`
	Enabled      bool      `json:"enabled"`
	// Joined fields populated by routing/detail queries:
	SIPUsername string `json:"sip_username,omitempty"`
	SIPDomain   string `json:"sip_domain,omitempty"`
	Extension   string `json:"extension,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

type CreateRingGroupInput struct {
	TenantID       uuid.UUID
	Extension      string
	Name           string
	Strategy       string // default "simultaneous" if blank
	RingTimeoutSec int    // default 30 if 0
	FallbackKind   string
	FallbackID     *uuid.UUID
	CallerIDPrefix string
}

func (s *Store) CreateRingGroup(ctx context.Context, in CreateRingGroupInput) (*RingGroup, error) {
	if in.Strategy == "" {
		in.Strategy = "simultaneous"
	}
	if in.RingTimeoutSec == 0 {
		in.RingTimeoutSec = 30
	}
	const q = `
		INSERT INTO ring_groups
		    (tenant_id, extension, name, strategy, ring_timeout_sec,
		     fallback_kind, fallback_id, caller_id_prefix)
		VALUES ($1, NULLIF($2,''), $3, $4, $5,
		        NULLIF($6,''), $7, NULLIF($8,''))
		RETURNING id, tenant_id, COALESCE(extension,''), name, strategy,
		          ring_timeout_sec, COALESCE(fallback_kind,''), fallback_id,
		          COALESCE(caller_id_prefix,''), enabled, created_at, updated_at`
	var rg RingGroup
	err := s.DB.QueryRow(ctx, q,
		in.TenantID, in.Extension, in.Name, in.Strategy, in.RingTimeoutSec,
		in.FallbackKind, in.FallbackID, in.CallerIDPrefix,
	).Scan(
		&rg.ID, &rg.TenantID, &rg.Extension, &rg.Name, &rg.Strategy,
		&rg.RingTimeoutSec, &rg.FallbackKind, &rg.FallbackID,
		&rg.CallerIDPrefix, &rg.Enabled, &rg.CreatedAt, &rg.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &rg, nil
}

func (s *Store) ListRingGroupsForTenant(ctx context.Context, tenantID uuid.UUID) ([]RingGroup, error) {
	const q = `
		SELECT id, tenant_id, COALESCE(extension,''), name, strategy,
		       ring_timeout_sec, COALESCE(fallback_kind,''), fallback_id,
		       COALESCE(caller_id_prefix,''), enabled, created_at, updated_at
		  FROM ring_groups WHERE tenant_id = $1
		 ORDER BY name`
	rows, err := s.DB.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RingGroup
	for rows.Next() {
		var rg RingGroup
		if err := rows.Scan(
			&rg.ID, &rg.TenantID, &rg.Extension, &rg.Name, &rg.Strategy,
			&rg.RingTimeoutSec, &rg.FallbackKind, &rg.FallbackID,
			&rg.CallerIDPrefix, &rg.Enabled, &rg.CreatedAt, &rg.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, rg)
	}
	return out, rows.Err()
}

type AddRingGroupMemberInput struct {
	RingGroupID  uuid.UUID
	ExtensionID  uuid.UUID
	Priority     int
	RingDelaySec int
}

func (s *Store) AddRingGroupMember(ctx context.Context, in AddRingGroupMemberInput) (*RingGroupMember, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Tenant safety: the extension must belong to the same tenant as the group.
	const checkQ = `
		SELECT rg.tenant_id = e.tenant_id
		  FROM ring_groups rg, extensions e
		 WHERE rg.id = $1 AND e.id = $2`
	var sameTenant bool
	if err := tx.QueryRow(ctx, checkQ, in.RingGroupID, in.ExtensionID).Scan(&sameTenant); err != nil {
		return nil, err
	}
	if !sameTenant {
		return nil, ErrCrossTenant
	}

	if in.Priority == 0 {
		in.Priority = 100
	}
	const ins = `
		INSERT INTO ring_group_members
		    (ring_group_id, extension_id, priority, ring_delay_sec)
		VALUES ($1, $2, $3, $4)
		RETURNING id, ring_group_id, extension_id, priority, ring_delay_sec, enabled`
	var m RingGroupMember
	err = tx.QueryRow(ctx, ins,
		in.RingGroupID, in.ExtensionID, in.Priority, in.RingDelaySec,
	).Scan(&m.ID, &m.RingGroupID, &m.ExtensionID, &m.Priority, &m.RingDelaySec, &m.Enabled)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &m, nil
}

// RingGroupRoutingInfo is everything the dialplan handler needs in one trip.
type RingGroupRoutingInfo struct {
	Group   RingGroup
	Members []RingGroupMember
}

// LookupRingGroupByExtension resolves an internal-dialed number (e.g. "200")
// to a ring group + its enabled members, ordered by priority asc.
//
// Returns pgx.ErrNoRows if no ring group has that extension for the tenant.
func (s *Store) LookupRingGroupByExtension(ctx context.Context, tenantDomain, ext string) (*RingGroupRoutingInfo, error) {
	const headerQ = `
		SELECT rg.id, rg.tenant_id, COALESCE(rg.extension,''), rg.name,
		       rg.strategy, rg.ring_timeout_sec,
		       COALESCE(rg.fallback_kind,''), rg.fallback_id,
		       COALESCE(rg.caller_id_prefix,''),
		       rg.enabled, rg.created_at, rg.updated_at
		  FROM ring_groups rg
		  JOIN sip_domains sd ON sd.tenant_id = rg.tenant_id
		 WHERE sd.domain = $1 AND rg.extension = $2 AND rg.enabled = true
		 LIMIT 1`
	var rg RingGroup
	err := s.DB.QueryRow(ctx, headerQ, tenantDomain, ext).Scan(
		&rg.ID, &rg.TenantID, &rg.Extension, &rg.Name,
		&rg.Strategy, &rg.RingTimeoutSec,
		&rg.FallbackKind, &rg.FallbackID, &rg.CallerIDPrefix,
		&rg.Enabled, &rg.CreatedAt, &rg.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	members, err := s.fetchRingGroupMembers(ctx, rg.ID)
	if err != nil {
		return nil, err
	}
	return &RingGroupRoutingInfo{Group: rg, Members: members}, nil
}

// LookupDIDRingGroupTarget resolves an inbound DID (E.164) whose
// destination_kind = 'ring_group' to its routing info.
func (s *Store) LookupDIDRingGroupTarget(ctx context.Context, e164 string) (*RingGroupRoutingInfo, error) {
	const headerQ = `
		SELECT rg.id, rg.tenant_id, COALESCE(rg.extension,''), rg.name,
		       rg.strategy, rg.ring_timeout_sec,
		       COALESCE(rg.fallback_kind,''), rg.fallback_id,
		       COALESCE(rg.caller_id_prefix,''),
		       rg.enabled, rg.created_at, rg.updated_at
		  FROM dids d
		  JOIN ring_groups rg ON rg.id = d.destination_id AND d.destination_kind = 'ring_group'
		 WHERE d.e164 = $1 AND d.enabled = true AND rg.enabled = true
		 LIMIT 1`
	var rg RingGroup
	err := s.DB.QueryRow(ctx, headerQ, e164).Scan(
		&rg.ID, &rg.TenantID, &rg.Extension, &rg.Name,
		&rg.Strategy, &rg.RingTimeoutSec,
		&rg.FallbackKind, &rg.FallbackID, &rg.CallerIDPrefix,
		&rg.Enabled, &rg.CreatedAt, &rg.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	members, err := s.fetchRingGroupMembers(ctx, rg.ID)
	if err != nil {
		return nil, err
	}
	return &RingGroupRoutingInfo{Group: rg, Members: members}, nil
}

func (s *Store) fetchRingGroupMembers(ctx context.Context, rgID uuid.UUID) ([]RingGroupMember, error) {
	const q = `
		SELECT rgm.id, rgm.ring_group_id, rgm.extension_id, rgm.priority,
		       rgm.ring_delay_sec, rgm.enabled,
		       e.sip_username, sd.domain, e.extension
		  FROM ring_group_members rgm
		  JOIN extensions  e  ON e.id  = rgm.extension_id
		  JOIN sip_domains sd ON sd.id = e.sip_domain_id
		 WHERE rgm.ring_group_id = $1
		   AND rgm.enabled = true
		   AND e.status = 'active'
		 ORDER BY rgm.priority, e.extension`
	rows, err := s.DB.Query(ctx, q, rgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RingGroupMember
	for rows.Next() {
		var m RingGroupMember
		if err := rows.Scan(
			&m.ID, &m.RingGroupID, &m.ExtensionID, &m.Priority,
			&m.RingDelaySec, &m.Enabled,
			&m.SIPUsername, &m.SIPDomain, &m.Extension,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ErrRingGroupEmpty is returned when a ring group exists but has no enabled
// members — the dialplan handler treats this as no-route.
var ErrRingGroupEmpty = errors.New("ring group has no enabled members")

// NextRingGroupRRIndex returns the next member index for round-robin
// rotation. State is kept in Redis (`ringgroup:<id>:rr`) so it survives
// control-plane restarts and stays consistent across HA replicas. Wraps
// around memberCount; safe under concurrent calls (Redis INCR is atomic).
//
// Returns 0 (and logs nothing) when memberCount==0 — caller should already
// have short-circuited on empty groups.
func (s *Store) NextRingGroupRRIndex(ctx context.Context, groupID uuid.UUID, memberCount int) (int, error) {
	if memberCount <= 0 {
		return 0, nil
	}
	key := "ringgroup:" + groupID.String() + ":rr"
	n, err := s.Redis.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	// INCR returns 1 on first call → start with index 0.
	return int((n - 1) % int64(memberCount)), nil
}
