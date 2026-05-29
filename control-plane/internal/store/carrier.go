package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

type Carrier struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind"`
	SIPProxyHost string    `json:"sip_proxy_host"`
	SIPProxyPort int       `json:"sip_proxy_port"`
	Transport    string    `json:"transport"`
	Enabled      bool      `json:"enabled"`
	Priority     int       `json:"priority"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type CarrierAccount struct {
	ID                uuid.UUID  `json:"id"`
	TenantID          *uuid.UUID `json:"tenant_id,omitempty"`
	CarrierID         uuid.UUID  `json:"carrier_id"`
	Name              string     `json:"name"`
	SIPUsername       string     `json:"sip_username"`
	SIPPassword       string     `json:"sip_password,omitempty"`
	AuthRealm         string     `json:"auth_realm,omitempty"`
	ProxyHostOverride string     `json:"proxy_host_override,omitempty"`
	ProxyPortOverride int        `json:"proxy_port_override,omitempty"`
	TransportOverride string     `json:"transport_override,omitempty"`
	FSGatewayName     string     `json:"fs_gateway_name"`
	Register          bool       `json:"register"`
	MainDIDE164       string     `json:"main_did_e164,omitempty"`
	Enabled           bool       `json:"enabled"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`

	// Convenience join: carrier kind ("callcentric", "telnyx", ...) and the
	// SIP proxy host. Populated by ListCarrierAccountsForTenant.
	CarrierKind             string `json:"carrier_kind,omitempty"`
	CarrierProxyHost        string `json:"carrier_proxy_host,omitempty"`
	CarrierTransport        string `json:"carrier_transport,omitempty"`
	CarrierDefaultAuthRealm string `json:"carrier_default_auth_realm,omitempty"`
}

func (s *Store) GetCarrierByKind(ctx context.Context, kind string) (*Carrier, error) {
	const q = `
		SELECT id, name, kind, sip_proxy_host, sip_proxy_port, transport,
		       enabled, priority, created_at, updated_at
		  FROM carriers WHERE kind = $1 AND enabled = true
		 ORDER BY priority, created_at LIMIT 1`
	var c Carrier
	err := s.DB.QueryRow(ctx, q, kind).Scan(
		&c.ID, &c.Name, &c.Kind, &c.SIPProxyHost, &c.SIPProxyPort, &c.Transport,
		&c.Enabled, &c.Priority, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) ListCarriers(ctx context.Context) ([]Carrier, error) {
	const q = `
		SELECT id, name, kind, sip_proxy_host, sip_proxy_port, transport,
		       enabled, priority, created_at, updated_at
		  FROM carriers ORDER BY priority, name`
	rows, err := s.DB.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Carrier
	for rows.Next() {
		var c Carrier
		if err := rows.Scan(
			&c.ID, &c.Name, &c.Kind, &c.SIPProxyHost, &c.SIPProxyPort, &c.Transport,
			&c.Enabled, &c.Priority, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

type CreateCarrierAccountInput struct {
	TenantID          *uuid.UUID // Phase 5.1: required for new tenant-owned trunks
	CarrierID         uuid.UUID
	Name              string
	SIPUsername       string
	SIPPassword       string
	AuthRealm         string
	ProxyHostOverride string // e.g. alpha.callcentric.com when the carrier puts you on a specific cluster
	ProxyPortOverride int    // 0 = use carrier default (typically 5060)
	TransportOverride string // "udp" | "tcp" | "tls" | "" (use carrier default)
	FSGatewayName     string
	Register          bool
	MainDIDE164       string
}

func (s *Store) CreateCarrierAccount(ctx context.Context, in CreateCarrierAccountInput) (*CarrierAccount, error) {
	const q = `
		INSERT INTO carrier_accounts
		    (tenant_id, carrier_id, name, sip_username, sip_password, auth_realm,
		     proxy_host_override, proxy_port_override, transport_override,
		     fs_gateway_name, register, main_did_e164)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6,''), NULLIF($7,''),
		        NULLIF($8,0), NULLIF($9,''),
		        $10, $11, NULLIF($12,''))
		RETURNING id, tenant_id, carrier_id, name, sip_username, sip_password,
		          COALESCE(auth_realm,''), COALESCE(proxy_host_override,''),
		          COALESCE(proxy_port_override,0), COALESCE(transport_override,''),
		          fs_gateway_name, register,
		          COALESCE(main_did_e164,''), enabled, created_at, updated_at`
	var a CarrierAccount
	err := s.DB.QueryRow(ctx, q,
		in.TenantID, in.CarrierID, in.Name, in.SIPUsername, in.SIPPassword, in.AuthRealm,
		in.ProxyHostOverride, in.ProxyPortOverride, in.TransportOverride,
		in.FSGatewayName, in.Register, in.MainDIDE164,
	).Scan(
		&a.ID, &a.TenantID, &a.CarrierID, &a.Name, &a.SIPUsername, &a.SIPPassword,
		&a.AuthRealm, &a.ProxyHostOverride, &a.ProxyPortOverride, &a.TransportOverride,
		&a.FSGatewayName, &a.Register,
		&a.MainDIDE164, &a.Enabled, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ListCarrierAccountsForTenant returns every trunk for a tenant + a couple
// of fields from the parent carrier (so the portal can display "CallCentric"
// not just the carrier UUID).
func (s *Store) ListCarrierAccountsForTenant(ctx context.Context, tenantID uuid.UUID) ([]CarrierAccount, error) {
	const q = `
		SELECT ca.id, ca.tenant_id, ca.carrier_id, ca.name, ca.sip_username,
		       '', COALESCE(ca.auth_realm,''),
		       COALESCE(ca.proxy_host_override,''),
		       COALESCE(ca.proxy_port_override,0),
		       COALESCE(ca.transport_override,''),
		       ca.fs_gateway_name, ca.register, COALESCE(ca.main_did_e164,''),
		       ca.enabled, ca.created_at, ca.updated_at,
		       c.kind, c.sip_proxy_host, c.transport,
		       COALESCE(c.default_auth_realm,'')
		  FROM carrier_accounts ca
		  JOIN carriers c ON c.id = ca.carrier_id
		 WHERE ca.tenant_id = $1
		 ORDER BY ca.created_at DESC`
	rows, err := s.DB.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CarrierAccount
	for rows.Next() {
		var a CarrierAccount
		if err := rows.Scan(
			&a.ID, &a.TenantID, &a.CarrierID, &a.Name, &a.SIPUsername,
			&a.SIPPassword, &a.AuthRealm,
			&a.ProxyHostOverride, &a.ProxyPortOverride, &a.TransportOverride,
			&a.FSGatewayName, &a.Register,
			&a.MainDIDE164, &a.Enabled, &a.CreatedAt, &a.UpdatedAt,
			&a.CarrierKind, &a.CarrierProxyHost, &a.CarrierTransport,
			&a.CarrierDefaultAuthRealm,
		); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetCarrierAccountForTenant — tenant-scoped lookup (refuses if the carrier
// account belongs to a different tenant).
func (s *Store) GetCarrierAccountForTenant(ctx context.Context, tenantID, id uuid.UUID) (*CarrierAccount, error) {
	const q = `
		SELECT ca.id, ca.tenant_id, ca.carrier_id, ca.name, ca.sip_username,
		       ca.sip_password, COALESCE(ca.auth_realm,''),
		       COALESCE(ca.proxy_host_override,''),
		       COALESCE(ca.proxy_port_override,0),
		       COALESCE(ca.transport_override,''),
		       ca.fs_gateway_name, ca.register, COALESCE(ca.main_did_e164,''),
		       ca.enabled, ca.created_at, ca.updated_at,
		       c.kind, c.sip_proxy_host, c.transport,
		       COALESCE(c.default_auth_realm,'')
		  FROM carrier_accounts ca
		  JOIN carriers c ON c.id = ca.carrier_id
		 WHERE ca.tenant_id = $1 AND ca.id = $2`
	var a CarrierAccount
	err := s.DB.QueryRow(ctx, q, tenantID, id).Scan(
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
	return &a, nil
}

// UpdateCarrierAccountForTenant — change creds / state. nil password leaves
// the stored value untouched (matches the SSO config pattern).
type UpdateCarrierAccountInput struct {
	Name              string
	SIPUsername       string
	SIPPassword       string // empty → keep existing
	AuthRealm         string
	ProxyHostOverride string
	ProxyPortOverride int
	TransportOverride string
	Register          bool
	MainDIDE164       string
	Enabled           bool
}

func (s *Store) UpdateCarrierAccountForTenant(ctx context.Context, tenantID, id uuid.UUID, in UpdateCarrierAccountInput) error {
	const q = `
		UPDATE carrier_accounts
		   SET name = $3, sip_username = $4,
		       sip_password = CASE WHEN length($5) > 0 THEN $5 ELSE sip_password END,
		       auth_realm = NULLIF($6,''),
		       proxy_host_override = NULLIF($7,''),
		       proxy_port_override = NULLIF($8,0),
		       transport_override = NULLIF($9,''),
		       register = $10, main_did_e164 = NULLIF($11,''), enabled = $12,
		       updated_at = now()
		 WHERE tenant_id = $1 AND id = $2`
	tag, err := s.DB.Exec(ctx, q,
		tenantID, id, in.Name, in.SIPUsername, in.SIPPassword, in.AuthRealm,
		in.ProxyHostOverride, in.ProxyPortOverride, in.TransportOverride,
		in.Register, in.MainDIDE164, in.Enabled,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("carrier account not found for this tenant")
	}
	return nil
}

// DeleteCarrierAccountForTenant removes the trunk + every DID that routes
// inbound calls through it. Atomic. The FK from dids.carrier_account_id
// otherwise blocks the delete; cascading inside a single tx matches the
// "delete this trunk and everything that depends on it" intent admins have
// when they hit the button.
//
// Returns the number of DIDs that were cascaded so the caller can include
// it in the success flash ("trunk + 3 numbers removed").
func (s *Store) DeleteCarrierAccountForTenant(ctx context.Context, tenantID, id uuid.UUID) (cascadedDIDs int, err error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	didTag, err := tx.Exec(ctx,
		`DELETE FROM dids WHERE tenant_id = $1 AND carrier_account_id = $2`,
		tenantID, id)
	if err != nil {
		return 0, err
	}
	cascadedDIDs = int(didTag.RowsAffected())

	tag, err := tx.Exec(ctx,
		`DELETE FROM carrier_accounts WHERE tenant_id = $1 AND id = $2`,
		tenantID, id)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 0 {
		return 0, errors.New("carrier account not found for this tenant")
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return cascadedDIDs, nil
}

// ListAllEnabledCarrierAccounts returns every enabled trunk across all
// tenants — used by the gateway provisioner to materialize XML.
func (s *Store) ListAllEnabledCarrierAccounts(ctx context.Context) ([]CarrierAccount, error) {
	const q = `
		SELECT ca.id, ca.tenant_id, ca.carrier_id, ca.name, ca.sip_username,
		       ca.sip_password, COALESCE(ca.auth_realm,''),
		       COALESCE(ca.proxy_host_override,''),
		       COALESCE(ca.proxy_port_override,0),
		       COALESCE(ca.transport_override,''),
		       ca.fs_gateway_name, ca.register, COALESCE(ca.main_did_e164,''),
		       ca.enabled, ca.created_at, ca.updated_at,
		       c.kind, c.sip_proxy_host, c.transport,
		       COALESCE(c.default_auth_realm,'')
		  FROM carrier_accounts ca
		  JOIN carriers c ON c.id = ca.carrier_id
		 WHERE ca.enabled = true AND c.enabled = true
		 ORDER BY ca.created_at`
	rows, err := s.DB.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CarrierAccount
	for rows.Next() {
		var a CarrierAccount
		if err := rows.Scan(
			&a.ID, &a.TenantID, &a.CarrierID, &a.Name, &a.SIPUsername,
			&a.SIPPassword, &a.AuthRealm,
			&a.ProxyHostOverride, &a.ProxyPortOverride, &a.TransportOverride,
			&a.FSGatewayName, &a.Register,
			&a.MainDIDE164, &a.Enabled, &a.CreatedAt, &a.UpdatedAt,
			&a.CarrierKind, &a.CarrierProxyHost, &a.CarrierTransport,
			&a.CarrierDefaultAuthRealm,
		); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// PickPrimaryCarrierAccountForTenant returns the first enabled trunk for a
// tenant. Phase 5.1 outbound routing: when an extension dials E.164, route
// via this trunk. Later waves add per-route configuration.
func (s *Store) PickPrimaryCarrierAccountForTenant(ctx context.Context, tenantID uuid.UUID) (*CarrierAccount, error) {
	const q = `
		SELECT ca.id, ca.tenant_id, ca.carrier_id, ca.name, ca.sip_username,
		       '', COALESCE(ca.auth_realm,''),
		       COALESCE(ca.proxy_host_override,''),
		       COALESCE(ca.proxy_port_override,0),
		       COALESCE(ca.transport_override,''),
		       ca.fs_gateway_name, ca.register, COALESCE(ca.main_did_e164,''),
		       ca.enabled, ca.created_at, ca.updated_at,
		       c.kind, c.sip_proxy_host, c.transport,
		       COALESCE(c.default_auth_realm,'')
		  FROM carrier_accounts ca
		  JOIN carriers c ON c.id = ca.carrier_id
		 WHERE ca.tenant_id = $1 AND ca.enabled = true AND c.enabled = true
		 ORDER BY c.priority, ca.created_at LIMIT 1`
	var a CarrierAccount
	err := s.DB.QueryRow(ctx, q, tenantID).Scan(
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
	return &a, nil
}

// PickPrimaryCarrierAccount returns the first enabled carrier_account ordered
// by carrier priority. Phase 2 single-carrier helper; Phase 3 uses per-tenant
// outbound_routes for proper routing.
func (s *Store) PickPrimaryCarrierAccount(ctx context.Context) (*CarrierAccount, error) {
	const q = `
		SELECT ca.id, ca.carrier_id, ca.name, ca.sip_username, '',
		       COALESCE(ca.auth_realm,''),
		       COALESCE(ca.proxy_host_override,''),
		       COALESCE(ca.proxy_port_override,0),
		       COALESCE(ca.transport_override,''),
		       ca.fs_gateway_name, ca.register,
		       COALESCE(ca.main_did_e164,''), ca.enabled,
		       ca.created_at, ca.updated_at
		  FROM carrier_accounts ca
		  JOIN carriers c ON c.id = ca.carrier_id
		 WHERE ca.enabled = true AND c.enabled = true
		 ORDER BY c.priority, ca.created_at
		 LIMIT 1`
	var a CarrierAccount
	err := s.DB.QueryRow(ctx, q).Scan(
		&a.ID, &a.CarrierID, &a.Name, &a.SIPUsername, &a.SIPPassword,
		&a.AuthRealm,
		&a.ProxyHostOverride, &a.ProxyPortOverride, &a.TransportOverride,
		&a.FSGatewayName, &a.Register,
		&a.MainDIDE164, &a.Enabled, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &a, nil
}
