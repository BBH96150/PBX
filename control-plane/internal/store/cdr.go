package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// CDR mirrors the cdrs table. raw is the full event payload for forensics.
type CDR struct {
	ID            uuid.UUID
	TenantID      *uuid.UUID
	CallUUID      string
	Direction     string // inbound | outbound | internal
	FromURI       string
	ToURI         string
	CallerIDNum   string
	CallerIDName  string
	StartedAt     time.Time
	AnsweredAt    *time.Time
	EndedAt       *time.Time
	DurationSec   *int
	BillableSec   *int
	Disposition   *string
	HangupCause   string
	CarrierID     *uuid.UUID
	RecordingPath string
	Note          string
	// DispositionCodeID is the tenant-defined wrap-up code assigned to this call
	// (nil = none). DispositionLabel/DispositionColor are the joined display
	// fields for that code, populated by the list/get queries; they are empty
	// when no code is assigned.
	DispositionCodeID *uuid.UUID
	DispositionLabel  string
	DispositionColor  string
	Raw               map[string]string
}

// ListCDRsForTenant returns recent CDRs for a tenant ordered by start time
// descending. limit is hard-capped at 500 to keep the portal page
// reasonable.
func (s *Store) ListCDRsForTenant(ctx context.Context, tenantID uuid.UUID, limit int) ([]CDR, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	const q = `
		SELECT c.id, c.tenant_id, c.call_uuid, c.direction, c.from_uri, c.to_uri,
		       COALESCE(c.caller_id_num,''), COALESCE(c.caller_id_name,''),
		       c.started_at, c.answered_at, c.ended_at,
		       c.duration_sec, c.billable_sec, c.disposition,
		       COALESCE(c.hangup_cause,''), c.carrier_id, COALESCE(c.recording_path,''), COALESCE(c.note,''),
		       c.disposition_code_id, COALESCE(dc.label,''), COALESCE(dc.color,''),
		       c.raw
		  FROM cdrs c
		  LEFT JOIN disposition_codes dc ON dc.id = c.disposition_code_id
		 WHERE c.tenant_id = $1
		 ORDER BY c.started_at DESC LIMIT $2`
	rows, err := s.DB.Query(ctx, q, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CDR
	for rows.Next() {
		var c CDR
		var rawJSON []byte
		if err := rows.Scan(
			&c.ID, &c.TenantID, &c.CallUUID, &c.Direction, &c.FromURI, &c.ToURI,
			&c.CallerIDNum, &c.CallerIDName,
			&c.StartedAt, &c.AnsweredAt, &c.EndedAt,
			&c.DurationSec, &c.BillableSec, &c.Disposition,
			&c.HangupCause, &c.CarrierID, &c.RecordingPath, &c.Note,
			&c.DispositionCodeID, &c.DispositionLabel, &c.DispositionColor,
			&rawJSON,
		); err != nil {
			return nil, err
		}
		// Raw left nil — UI doesn't need it.
		out = append(out, c)
	}
	return out, rows.Err()
}

// RecentCDR is a compact CDR row joined with its tenant name, for the
// platform dashboard's recent-activity feed.
type RecentCDR struct {
	TenantID    *uuid.UUID
	TenantName  string
	Direction   string
	CallerIDNum string
	ToURI       string
	StartedAt   time.Time
	DurationSec *int
	Disposition *string
}

// ListRecentCDRs returns the most recent calls across all tenants, or scoped to
// one tenant when scope is non-nil. Used by the dashboard activity feed.
func (s *Store) ListRecentCDRs(ctx context.Context, scope *uuid.UUID, limit int) ([]RecentCDR, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	const q = `
		SELECT c.tenant_id, COALESCE(t.name,'—'), c.direction,
		       COALESCE(c.caller_id_num,''), c.to_uri, c.started_at,
		       c.duration_sec, c.disposition
		  FROM cdrs c
		  LEFT JOIN tenants t ON t.id = c.tenant_id
		 WHERE ($1::uuid IS NULL OR c.tenant_id = $1)
		 ORDER BY c.started_at DESC LIMIT $2`
	rows, err := s.DB.Query(ctx, q, scope, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecentCDR
	for rows.Next() {
		var c RecentCDR
		if err := rows.Scan(&c.TenantID, &c.TenantName, &c.Direction,
			&c.CallerIDNum, &c.ToURI, &c.StartedAt, &c.DurationSec, &c.Disposition); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CDRFilter narrows a CDR query. Empty/nil fields are ignored.
type CDRFilter struct {
	Direction string     // "" = all; else inbound|outbound|internal
	Search    string     // matches from/to/caller number or name (ILIKE)
	Since     *time.Time // started_at >= Since
	Until     *time.Time // started_at < Until
	Limit     int        // capped at 500
}

// ListCDRsFilteredForTenant returns CDRs matching the filter, newest first.
// All filter clauses are parameterized (no dynamic SQL).
func (s *Store) ListCDRsFilteredForTenant(ctx context.Context, tenantID uuid.UUID, f CDRFilter) ([]CDR, error) {
	// Default to a page-sized 200; clamp (don't reset) larger requests so the
	// CSV export can ask for up to 10k rows without silently dropping to 200.
	if f.Limit <= 0 {
		f.Limit = 200
	}
	if f.Limit > 10000 {
		f.Limit = 10000
	}
	const q = `
		SELECT c.id, c.tenant_id, c.call_uuid, c.direction, c.from_uri, c.to_uri,
		       COALESCE(c.caller_id_num,''), COALESCE(c.caller_id_name,''),
		       c.started_at, c.answered_at, c.ended_at,
		       c.duration_sec, c.billable_sec, c.disposition,
		       COALESCE(c.hangup_cause,''), c.carrier_id, COALESCE(c.recording_path,''), COALESCE(c.note,''),
		       c.disposition_code_id, COALESCE(dc.label,''), COALESCE(dc.color,''),
		       c.raw
		  FROM cdrs c
		  LEFT JOIN disposition_codes dc ON dc.id = c.disposition_code_id
		 WHERE c.tenant_id = $1
		   AND ($2 = '' OR c.direction = $2)
		   AND ($3 = '' OR c.from_uri ILIKE '%'||$3||'%' OR c.to_uri ILIKE '%'||$3||'%'
		        OR COALESCE(c.caller_id_num,'') ILIKE '%'||$3||'%'
		        OR COALESCE(c.caller_id_name,'') ILIKE '%'||$3||'%')
		   AND ($4::timestamptz IS NULL OR c.started_at >= $4)
		   AND ($5::timestamptz IS NULL OR c.started_at < $5)
		 ORDER BY c.started_at DESC LIMIT $6`
	rows, err := s.DB.Query(ctx, q, tenantID, f.Direction, f.Search, f.Since, f.Until, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CDR
	for rows.Next() {
		var c CDR
		var rawJSON []byte
		if err := rows.Scan(
			&c.ID, &c.TenantID, &c.CallUUID, &c.Direction, &c.FromURI, &c.ToURI,
			&c.CallerIDNum, &c.CallerIDName,
			&c.StartedAt, &c.AnsweredAt, &c.EndedAt,
			&c.DurationSec, &c.BillableSec, &c.Disposition,
			&c.HangupCause, &c.CarrierID, &c.RecordingPath, &c.Note,
			&c.DispositionCodeID, &c.DispositionLabel, &c.DispositionColor,
			&rawJSON,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCDRForTenant fetches one CDR scoped to a tenant — used before streaming or
// deleting its recording.
func (s *Store) GetCDRForTenant(ctx context.Context, tenantID, cdrID uuid.UUID) (*CDR, error) {
	const q = `
		SELECT c.id, c.tenant_id, c.call_uuid, c.direction, c.from_uri, c.to_uri,
		       COALESCE(c.caller_id_num,''), COALESCE(c.caller_id_name,''),
		       c.started_at, c.answered_at, c.ended_at,
		       c.duration_sec, c.billable_sec, c.disposition,
		       COALESCE(c.hangup_cause,''), c.carrier_id, COALESCE(c.recording_path,''), COALESCE(c.note,''),
		       c.disposition_code_id, COALESCE(dc.label,''), COALESCE(dc.color,''), c.raw
		  FROM cdrs c
		  LEFT JOIN disposition_codes dc ON dc.id = c.disposition_code_id
		 WHERE c.id = $1 AND c.tenant_id = $2`
	var c CDR
	var rawJSON []byte
	err := s.DB.QueryRow(ctx, q, cdrID, tenantID).Scan(
		&c.ID, &c.TenantID, &c.CallUUID, &c.Direction, &c.FromURI, &c.ToURI,
		&c.CallerIDNum, &c.CallerIDName,
		&c.StartedAt, &c.AnsweredAt, &c.EndedAt,
		&c.DurationSec, &c.BillableSec, &c.Disposition,
		&c.HangupCause, &c.CarrierID, &c.RecordingPath, &c.Note,
		&c.DispositionCodeID, &c.DispositionLabel, &c.DispositionColor, &rawJSON,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// SetCDRNote sets (or clears, with "") a free-text note/tag on a call record.
func (s *Store) SetCDRNote(ctx context.Context, tenantID, cdrID uuid.UUID, note string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE cdrs SET note = NULLIF($3,'') WHERE id = $1 AND tenant_id = $2`, cdrID, tenantID, note)
	return err
}

// SetCDRDisposition assigns (or clears, when codeID is nil) a tenant-defined
// wrap-up code to a call, tenant-scoped. A non-nil codeID must ALSO belong to the
// tenant — enforced via the subquery so a cross-tenant code can't be linked
// (mirrors SetExtensionE911Location). Clearing (codeID == nil) is supported: the
// subquery yields NULL and the column is set to NULL. Returns ErrCrossTenant if
// the CDR doesn't belong to the tenant (RowsAffected == 0).
func (s *Store) SetCDRDisposition(ctx context.Context, tenantID, cdrID uuid.UUID, codeID *uuid.UUID) error {
	const q = `
		UPDATE cdrs
		   SET disposition_code_id = (
		         SELECT id FROM disposition_codes
		          WHERE id = $3 AND tenant_id = $2
		       )
		 WHERE id = $1 AND tenant_id = $2`
	ct, err := s.DB.Exec(ctx, q, cdrID, tenantID, codeID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrCrossTenant
	}
	return nil
}

// ClearCDRRecording blanks a CDR's recording_path (after the file is deleted),
// tenant-scoped.
func (s *Store) ClearCDRRecording(ctx context.Context, tenantID, cdrID uuid.UUID) error {
	tag, err := s.DB.Exec(ctx,
		`UPDATE cdrs SET recording_path = NULL WHERE id = $1 AND tenant_id = $2`, cdrID, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("CDR not found for this tenant")
	}
	return nil
}

// CreateCDR inserts a CDR. ON CONFLICT (call_uuid) DO NOTHING absorbs the
// duplicate writes that happen when both call legs fire CHANNEL_HANGUP_COMPLETE
// (Phase 2 we only listen to A-leg, but defensive anyway).
func (s *Store) CreateCDR(ctx context.Context, c *CDR) error {
	raw, err := json.Marshal(c.Raw)
	if err != nil {
		return err
	}
	const q = `
		INSERT INTO cdrs (
			tenant_id, call_uuid, direction, from_uri, to_uri,
			caller_id_num, caller_id_name,
			started_at, answered_at, ended_at,
			duration_sec, billable_sec,
			disposition, hangup_cause, carrier_id,
			recording_path, raw
		) VALUES (
			$1,$2,$3,$4,$5,
			$6,$7,
			$8,$9,$10,
			$11,$12,
			$13,$14,$15,
			NULLIF($16,''),$17
		)
		ON CONFLICT (call_uuid) DO NOTHING`
	_, err = s.DB.Exec(ctx, q,
		c.TenantID, c.CallUUID, c.Direction, c.FromURI, c.ToURI,
		c.CallerIDNum, c.CallerIDName,
		c.StartedAt, c.AnsweredAt, c.EndedAt,
		c.DurationSec, c.BillableSec,
		c.Disposition, c.HangupCause, c.CarrierID,
		c.RecordingPath, raw,
	)
	return err
}
