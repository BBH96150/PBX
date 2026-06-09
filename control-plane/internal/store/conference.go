package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ConferenceRoom is a meet-me dial-in conference bridge (see migration 0036).
// Anyone who dials Extension within the tenant is prompted for the optional
// member/moderator PIN and joined into a FreeSWITCH conference.
type ConferenceRoom struct {
	ID            uuid.UUID `json:"id"`
	TenantID      uuid.UUID `json:"tenant_id"`
	Extension     string    `json:"extension"`
	Name          string    `json:"name"`
	PIN           string    `json:"pin,omitempty"`
	ModeratorPIN  string    `json:"moderator_pin,omitempty"`
	MaxMembers    int       `json:"max_members"`
	Record        bool      `json:"record"`
	AnnounceCount bool      `json:"announce_count"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CreateConferenceRoomInput struct {
	TenantID      uuid.UUID
	Extension     string
	Name          string
	PIN           string
	ModeratorPIN  string
	MaxMembers    int
	Record        bool
	AnnounceCount bool
}

func (s *Store) CreateConferenceRoom(ctx context.Context, in CreateConferenceRoomInput) (*ConferenceRoom, error) {
	const q = `
		INSERT INTO conference_rooms
		    (tenant_id, extension, name, pin, moderator_pin, max_members, record, announce_count)
		VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''), $6, $7, $8)
		RETURNING id, tenant_id, extension, name,
		          COALESCE(pin,''), COALESCE(moderator_pin,''),
		          max_members, record, announce_count, enabled, created_at, updated_at`
	var c ConferenceRoom
	err := s.DB.QueryRow(ctx, q,
		in.TenantID, in.Extension, in.Name, in.PIN, in.ModeratorPIN,
		in.MaxMembers, in.Record, in.AnnounceCount,
	).Scan(
		&c.ID, &c.TenantID, &c.Extension, &c.Name, &c.PIN, &c.ModeratorPIN,
		&c.MaxMembers, &c.Record, &c.AnnounceCount, &c.Enabled, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) ListConferenceRoomsForTenant(ctx context.Context, tenantID uuid.UUID) ([]ConferenceRoom, error) {
	const q = `
		SELECT id, tenant_id, extension, name,
		       COALESCE(pin,''), COALESCE(moderator_pin,''),
		       max_members, record, announce_count, enabled, created_at, updated_at
		  FROM conference_rooms WHERE tenant_id = $1
		 ORDER BY name`
	rows, err := s.DB.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConferenceRoom
	for rows.Next() {
		var c ConferenceRoom
		if err := rows.Scan(
			&c.ID, &c.TenantID, &c.Extension, &c.Name, &c.PIN, &c.ModeratorPIN,
			&c.MaxMembers, &c.Record, &c.AnnounceCount, &c.Enabled, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetConferenceRoomForTenant fetches one room, enforcing tenant ownership.
// Returns pgx.ErrNoRows if not found for that tenant.
func (s *Store) GetConferenceRoomForTenant(ctx context.Context, tenantID, id uuid.UUID) (*ConferenceRoom, error) {
	const q = `
		SELECT id, tenant_id, extension, name,
		       COALESCE(pin,''), COALESCE(moderator_pin,''),
		       max_members, record, announce_count, enabled, created_at, updated_at
		  FROM conference_rooms WHERE id = $1 AND tenant_id = $2`
	var c ConferenceRoom
	err := s.DB.QueryRow(ctx, q, id, tenantID).Scan(
		&c.ID, &c.TenantID, &c.Extension, &c.Name, &c.PIN, &c.ModeratorPIN,
		&c.MaxMembers, &c.Record, &c.AnnounceCount, &c.Enabled, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// DeleteConferenceRoomForTenant removes a room, tenant-scoped.
func (s *Store) DeleteConferenceRoomForTenant(ctx context.Context, tenantID, id uuid.UUID) error {
	const q = `DELETE FROM conference_rooms WHERE id = $1 AND tenant_id = $2`
	ct, err := s.DB.Exec(ctx, q, id, tenantID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrCrossTenant
	}
	return nil
}

// SetConferenceRoomEnabled flips a room's enabled flag, tenant-scoped.
func (s *Store) SetConferenceRoomEnabled(ctx context.Context, tenantID, id uuid.UUID, enabled bool) error {
	const q = `UPDATE conference_rooms SET enabled = $3, updated_at = now()
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

// LookupConferenceRoomByExtension resolves an internal-dialed room number
// within a tenant domain to an enabled conference room. Returns pgx.ErrNoRows
// if no enabled room has that extension. Used by the FS xml_curl dialplan
// handler.
func (s *Store) LookupConferenceRoomByExtension(ctx context.Context, tenantDomain, ext string) (*ConferenceRoom, error) {
	const q = `
		SELECT cr.id, cr.tenant_id, cr.extension, cr.name,
		       COALESCE(cr.pin,''), COALESCE(cr.moderator_pin,''),
		       cr.max_members, cr.record, cr.announce_count, cr.enabled,
		       cr.created_at, cr.updated_at
		  FROM conference_rooms cr
		  JOIN sip_domains sd ON sd.tenant_id = cr.tenant_id
		 WHERE sd.domain = $1 AND cr.extension = $2 AND cr.enabled = true
		 LIMIT 1`
	var c ConferenceRoom
	err := s.DB.QueryRow(ctx, q, tenantDomain, ext).Scan(
		&c.ID, &c.TenantID, &c.Extension, &c.Name, &c.PIN, &c.ModeratorPIN,
		&c.MaxMembers, &c.Record, &c.AnnounceCount, &c.Enabled, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}
