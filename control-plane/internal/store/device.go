package store

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Device struct {
	MAC               string     `json:"mac"`
	TenantID          uuid.UUID  `json:"tenant_id"`
	Vendor            string     `json:"vendor"`
	Model             string     `json:"model"`
	Firmware          string     `json:"firmware,omitempty"`
	ProvisioningToken string     `json:"provisioning_token,omitempty"`
	Label             string     `json:"label,omitempty"`
	LastProvisionedAt *time.Time `json:"last_provisioned_at,omitempty"`
	LastProvisionedIP string     `json:"last_provisioned_ip,omitempty"`
	UserAgent         string     `json:"user_agent,omitempty"`
	// Task #10 (RPS / true ZTP) sync state.
	RPSSyncedAt  *time.Time `json:"rps_synced_at,omitempty"`
	RPSLastError string     `json:"rps_last_error,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type DeviceLine struct {
	ID          uuid.UUID `json:"id"`
	DeviceMAC   string    `json:"device_mac"`
	LineNumber  int       `json:"line_number"`
	ExtensionID uuid.UUID `json:"extension_id"`
	Label       string    `json:"label,omitempty"`
	// Joined fields populated by detail queries:
	Extension   string `json:"extension,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

// ListDeviceLinesDetailed returns a device's bound lines with each line's
// extension number + display name, ordered by line number.
func (s *Store) ListDeviceLinesDetailed(ctx context.Context, mac string) ([]DeviceLine, error) {
	normMAC, err := normalizeMAC(mac)
	if err != nil {
		return nil, err
	}
	const q = `
		SELECT dl.id, dl.device_mac::text, dl.line_number, dl.extension_id,
		       COALESCE(dl.label,''), e.extension, COALESCE(e.display_name,'')
		  FROM device_lines dl
		  JOIN extensions e ON e.id = dl.extension_id
		 WHERE dl.device_mac = $1::macaddr
		 ORDER BY dl.line_number`
	rows, err := s.DB.Query(ctx, q, normMAC)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeviceLine
	for rows.Next() {
		var dl DeviceLine
		if err := rows.Scan(&dl.ID, &dl.DeviceMAC, &dl.LineNumber, &dl.ExtensionID,
			&dl.Label, &dl.Extension, &dl.DisplayName); err != nil {
			return nil, err
		}
		out = append(out, dl)
	}
	return out, rows.Err()
}

// DeleteDeviceLineForTenant unbinds a line, scoped to the tenant that owns the
// device (defense in depth).
func (s *Store) DeleteDeviceLineForTenant(ctx context.Context, tenantID, lineID uuid.UUID) error {
	tag, err := s.DB.Exec(ctx, `
		DELETE FROM device_lines dl
		 USING devices d
		 WHERE dl.id = $1 AND dl.device_mac = d.mac AND d.tenant_id = $2`,
		lineID, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNestedNotFound
	}
	return nil
}

var supportedVendors = map[string]bool{
	"polycom":     true,
	"grandstream": true,
	"yealink":     true,
	"cisco":       true,
	"snom":        true,
	"fanvil":      true,
	"generic":     true,
}

func (s *Store) CreateDevice(ctx context.Context, tenantID uuid.UUID, mac, vendor, model, label string) (*Device, error) {
	normMAC, err := normalizeMAC(mac)
	if err != nil {
		return nil, err
	}
	vendor = strings.ToLower(vendor)
	if !supportedVendors[vendor] {
		return nil, errors.New("unsupported vendor")
	}

	const q = `
		INSERT INTO devices (mac, tenant_id, vendor, model, label)
		VALUES ($1::macaddr, $2, $3, $4, NULLIF($5,''))
		RETURNING mac::text, tenant_id, vendor, model, COALESCE(firmware,''),
		          provisioning_token, COALESCE(label,''),
		          last_provisioned_at, COALESCE(host(last_provisioned_ip),''),
		          COALESCE(user_agent,''),
		          rps_synced_at, COALESCE(rps_last_error,''),
		          created_at, updated_at`
	var d Device
	err = s.DB.QueryRow(ctx, q, normMAC, tenantID, vendor, model, label).Scan(
		&d.MAC, &d.TenantID, &d.Vendor, &d.Model, &d.Firmware,
		&d.ProvisioningToken, &d.Label,
		&d.LastProvisionedAt, &d.LastProvisionedIP, &d.UserAgent,
		&d.RPSSyncedAt, &d.RPSLastError,
		&d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) GetDevice(ctx context.Context, mac string) (*Device, error) {
	normMAC, err := normalizeMAC(mac)
	if err != nil {
		return nil, err
	}
	const q = `
		SELECT mac::text, tenant_id, vendor, model, COALESCE(firmware,''),
		       provisioning_token, COALESCE(label,''),
		       last_provisioned_at, COALESCE(host(last_provisioned_ip),''),
		       COALESCE(user_agent,''),
		       rps_synced_at, COALESCE(rps_last_error,''),
		       created_at, updated_at
		  FROM devices WHERE mac = $1::macaddr`
	var d Device
	err = s.DB.QueryRow(ctx, q, normMAC).Scan(
		&d.MAC, &d.TenantID, &d.Vendor, &d.Model, &d.Firmware,
		&d.ProvisioningToken, &d.Label,
		&d.LastProvisionedAt, &d.LastProvisionedIP, &d.UserAgent,
		&d.RPSSyncedAt, &d.RPSLastError,
		&d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// MarkRPSSynced timestamps a successful RPS push and clears any prior error.
func (s *Store) MarkRPSSynced(ctx context.Context, mac string) error {
	normMAC, err := normalizeMAC(mac)
	if err != nil {
		return err
	}
	const q = `
		UPDATE devices
		   SET rps_synced_at = now(), rps_last_error = NULL
		 WHERE mac = $1::macaddr`
	_, err = s.DB.Exec(ctx, q, normMAC)
	return err
}

// MarkRPSError records the failure reason so admins can see it in /v1/devices/{mac}.
func (s *Store) MarkRPSError(ctx context.Context, mac, errMsg string) error {
	normMAC, err := normalizeMAC(mac)
	if err != nil {
		return err
	}
	const q = `
		UPDATE devices
		   SET rps_last_error = LEFT($2, 1000)
		 WHERE mac = $1::macaddr`
	_, err = s.DB.Exec(ctx, q, normMAC, errMsg)
	return err
}

func (s *Store) RecordProvisioningHit(ctx context.Context, mac, ip, userAgent string) error {
	normMAC, err := normalizeMAC(mac)
	if err != nil {
		return err
	}
	const q = `
		UPDATE devices
		   SET last_provisioned_at = now(),
		       last_provisioned_ip = NULLIF($2, '')::inet,
		       user_agent          = NULLIF($3, '')
		 WHERE mac = $1::macaddr`
	_, err = s.DB.Exec(ctx, q, normMAC, ip, userAgent)
	return err
}

// normalizeMAC accepts MAC addresses in any common form and emits the
// Postgres-friendly colon-separated lowercase form (aa:bb:cc:dd:ee:ff).
func normalizeMAC(in string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(in))
	// Strip common separators
	cleaned := strings.NewReplacer(":", "", "-", "", ".", "", " ", "").Replace(s)
	if len(cleaned) != 12 {
		return "", errors.New("invalid MAC length")
	}
	hw, err := net.ParseMAC(insertColons(cleaned))
	if err != nil {
		return "", err
	}
	return hw.String(), nil
}

func insertColons(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(s[i : i+2])
	}
	return b.String()
}
