package store

import (
	"context"

	"github.com/google/uuid"
)

// DeviceConfig is everything the provisioning renderer needs about a device
// to produce a config file: the device itself, its tenant, and its bound
// lines (each with extension + sip_domain).
type DeviceConfig struct {
	Device DeviceInfo
	Tenant TenantInfo
	Lines  []LineInfo
}

type DeviceInfo struct {
	MAC      string
	Vendor   string
	Model    string
	Firmware string
	Label    string
	Token    string
}

type TenantInfo struct {
	ID   uuid.UUID
	Slug string
	Name string
}

type LineInfo struct {
	LineNumber  int
	Label       string
	ExtensionID uuid.UUID
	Extension   string
	SIPUsername string
	SIPPassword string
	SIPDomain   string
	DisplayName string
	VoicemailOn bool
}

// LookupDeviceConfig fetches a device with its tenant and all bound lines,
// ordered by line_number. Returns ErrNoRows from pgx if the device doesn't exist.
func (s *Store) LookupDeviceConfig(ctx context.Context, mac string) (*DeviceConfig, error) {
	normMAC, err := normalizeMAC(mac)
	if err != nil {
		return nil, err
	}

	const headerQ = `
		SELECT d.mac::text,
		       COALESCE(d.vendor,''), COALESCE(d.model,''),
		       COALESCE(d.firmware,''), COALESCE(d.label,''),
		       d.provisioning_token,
		       t.id, t.slug, t.name
		  FROM devices d
		  JOIN tenants t ON t.id = d.tenant_id
		 WHERE d.mac = $1::macaddr`
	cfg := &DeviceConfig{}
	err = s.DB.QueryRow(ctx, headerQ, normMAC).Scan(
		&cfg.Device.MAC, &cfg.Device.Vendor, &cfg.Device.Model,
		&cfg.Device.Firmware, &cfg.Device.Label, &cfg.Device.Token,
		&cfg.Tenant.ID, &cfg.Tenant.Slug, &cfg.Tenant.Name,
	)
	if err != nil {
		return nil, err
	}

	const linesQ = `
		SELECT dl.line_number, COALESCE(dl.label,''), dl.extension_id,
		       e.extension, e.sip_username, e.sip_password,
		       sd.domain,
		       COALESCE(e.display_name,''), e.voicemail_enabled
		  FROM device_lines dl
		  JOIN extensions   e  ON e.id = dl.extension_id
		  JOIN sip_domains  sd ON sd.id = e.sip_domain_id
		 WHERE dl.device_mac = $1::macaddr AND e.status = 'active'
		 ORDER BY dl.line_number`
	rows, err := s.DB.Query(ctx, linesQ, normMAC)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var l LineInfo
		if err := rows.Scan(
			&l.LineNumber, &l.Label, &l.ExtensionID,
			&l.Extension, &l.SIPUsername, &l.SIPPassword,
			&l.SIPDomain, &l.DisplayName, &l.VoicemailOn,
		); err != nil {
			return nil, err
		}
		cfg.Lines = append(cfg.Lines, l)
	}
	return cfg, rows.Err()
}

// CreateDeviceLine binds an extension to a physical line key on a device.
// Enforces that the extension belongs to the same tenant as the device.
func (s *Store) CreateDeviceLine(ctx context.Context, mac string, lineNumber int, extensionID uuid.UUID, label string) (*DeviceLine, error) {
	normMAC, err := normalizeMAC(mac)
	if err != nil {
		return nil, err
	}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const checkQ = `
		SELECT d.tenant_id = e.tenant_id
		  FROM devices d, extensions e
		 WHERE d.mac = $1::macaddr AND e.id = $2`
	var sameTenant bool
	if err := tx.QueryRow(ctx, checkQ, normMAC, extensionID).Scan(&sameTenant); err != nil {
		return nil, err
	}
	if !sameTenant {
		return nil, ErrCrossTenant
	}

	const insertQ = `
		INSERT INTO device_lines (device_mac, line_number, extension_id, label)
		VALUES ($1::macaddr, $2, $3, NULLIF($4,''))
		RETURNING id, device_mac::text, line_number, extension_id, COALESCE(label,'')`
	var dl DeviceLine
	err = tx.QueryRow(ctx, insertQ, normMAC, lineNumber, extensionID, label).Scan(
		&dl.ID, &dl.DeviceMAC, &dl.LineNumber, &dl.ExtensionID, &dl.Label,
	)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &dl, nil
}

// ErrCrossTenant is returned when an admin tries to bind an extension to a
// device that belongs to a different tenant.
var ErrCrossTenant = errCrossTenant{}

type errCrossTenant struct{}

func (errCrossTenant) Error() string { return "device and extension belong to different tenants" }
