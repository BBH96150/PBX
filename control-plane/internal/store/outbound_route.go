package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrOutboundRouteNotFound is returned when a tenant-scoped route op misses.
var ErrOutboundRouteNotFound = errors.New("outbound route not found for this tenant")

// OutboundRoute is a per-tenant rule mapping a dialed E.164 prefix to the
// trunk that should carry the call (and optionally the caller ID to present).
type OutboundRoute struct {
	ID               uuid.UUID `json:"id"`
	TenantID         uuid.UUID `json:"tenant_id"`
	Name             string    `json:"name"`
	MatchPrefix      string    `json:"match_prefix"`
	CarrierAccountID uuid.UUID `json:"carrier_account_id"`
	CallerIDE164     string    `json:"caller_id_e164,omitempty"`
	Priority         int       `json:"priority"`
	Enabled          bool      `json:"enabled"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// OutboundDecision is the dialplan's resolved choice: which trunk to bridge
// through plus the caller ID to present (CIDOverride empty = use the trunk's
// main DID).
type OutboundDecision struct {
	RouteID     uuid.UUID
	Account     CarrierAccount
	CIDOverride string
}

type CreateOutboundRouteInput struct {
	TenantID         uuid.UUID
	Name             string
	MatchPrefix      string
	CarrierAccountID uuid.UUID
	CallerIDE164     string
	Priority         int
}

func (s *Store) CreateOutboundRoute(ctx context.Context, in CreateOutboundRouteInput) (*OutboundRoute, error) {
	const q = `
		INSERT INTO outbound_routes
		    (tenant_id, name, match_prefix, carrier_account_id, caller_id_e164, priority)
		VALUES ($1, $2, $3, $4, NULLIF($5,''), $6)
		RETURNING id, tenant_id, name, match_prefix, carrier_account_id,
		          COALESCE(caller_id_e164,''), priority, enabled, created_at, updated_at`
	var r OutboundRoute
	err := s.DB.QueryRow(ctx, q,
		in.TenantID, in.Name, in.MatchPrefix, in.CarrierAccountID, in.CallerIDE164, in.Priority,
	).Scan(
		&r.ID, &r.TenantID, &r.Name, &r.MatchPrefix, &r.CarrierAccountID,
		&r.CallerIDE164, &r.Priority, &r.Enabled, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) ListOutboundRoutesForTenant(ctx context.Context, tenantID uuid.UUID) ([]OutboundRoute, error) {
	const q = `
		SELECT id, tenant_id, name, match_prefix, carrier_account_id,
		       COALESCE(caller_id_e164,''), priority, enabled, created_at, updated_at
		  FROM outbound_routes WHERE tenant_id = $1
		 ORDER BY length(match_prefix) DESC, priority, created_at`
	rows, err := s.DB.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboundRoute
	for rows.Next() {
		var r OutboundRoute
		if err := rows.Scan(
			&r.ID, &r.TenantID, &r.Name, &r.MatchPrefix, &r.CarrierAccountID,
			&r.CallerIDE164, &r.Priority, &r.Enabled, &r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) DeleteOutboundRouteForTenant(ctx context.Context, tenantID, id uuid.UUID) error {
	tag, err := s.DB.Exec(ctx,
		`DELETE FROM outbound_routes WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrOutboundRouteNotFound
	}
	return nil
}

func (s *Store) SetOutboundRouteEnabledForTenant(ctx context.Context, tenantID, id uuid.UUID, enabled bool) error {
	tag, err := s.DB.Exec(ctx,
		`UPDATE outbound_routes SET enabled = $3, updated_at = now() WHERE id = $1 AND tenant_id = $2`,
		id, tenantID, enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrOutboundRouteNotFound
	}
	return nil
}

// ResolveOutboundRouteForTenant picks the most specific enabled route whose
// match_prefix is a prefix of the dialed number (longest prefix wins, then
// lowest priority, then oldest). normalizedE164 must include the leading +.
// Returns pgx.ErrNoRows when the tenant has no matching route, so the caller
// can fall back to the legacy primary-carrier pick.
func (s *Store) ResolveOutboundRouteForTenant(ctx context.Context, tenantID uuid.UUID, normalizedE164 string) (*OutboundDecision, error) {
	const q = `
		SELECT orr.id, COALESCE(orr.caller_id_e164,''),
		       ca.id, ca.tenant_id, ca.carrier_id, ca.name, ca.sip_username,
		       '', COALESCE(ca.auth_realm,''),
		       COALESCE(ca.proxy_host_override,''),
		       COALESCE(ca.proxy_port_override,0),
		       COALESCE(ca.transport_override,''),
		       ca.fs_gateway_name, ca.register, COALESCE(ca.main_did_e164,''),
		       ca.enabled, ca.created_at, ca.updated_at,
		       c.kind, c.sip_proxy_host, c.transport,
		       COALESCE(c.default_auth_realm,'')
		  FROM outbound_routes orr
		  JOIN carrier_accounts ca ON ca.id = orr.carrier_account_id AND ca.enabled = true
		  JOIN carriers c ON c.id = ca.carrier_id AND c.enabled = true
		 WHERE orr.tenant_id = $1 AND orr.enabled = true
		   AND $2 LIKE orr.match_prefix || '%'
		 ORDER BY length(orr.match_prefix) DESC, orr.priority, orr.created_at
		 LIMIT 1`
	var d OutboundDecision
	var a CarrierAccount
	err := s.DB.QueryRow(ctx, q, tenantID, normalizedE164).Scan(
		&d.RouteID, &d.CIDOverride,
		&a.ID, &a.TenantID, &a.CarrierID, &a.Name, &a.SIPUsername,
		&a.SIPPassword, &a.AuthRealm,
		&a.ProxyHostOverride, &a.ProxyPortOverride, &a.TransportOverride,
		&a.FSGatewayName, &a.Register,
		&a.MainDIDE164, &a.Enabled, &a.CreatedAt, &a.UpdatedAt,
		&a.CarrierKind, &a.CarrierProxyHost, &a.CarrierTransport,
		&a.CarrierDefaultAuthRealm,
	)
	if err != nil {
		return nil, err
	}
	d.Account = a
	return &d, nil
}
