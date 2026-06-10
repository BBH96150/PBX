package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// CallInsight is the AI-generated result for one call: the transcript, a short
// summary, and extracted action items. Written by the insights worker, read by
// the portal/API.
type CallInsight struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    *uuid.UUID `json:"tenant_id"`
	CallUUID    string     `json:"call_uuid"`
	Transcript  string     `json:"transcript"`
	Summary     string     `json:"summary"`
	ActionItems []string   `json:"action_items"`
	CreatedAt   time.Time  `json:"created_at"`
}

// CreateCallInsight upserts the insight for a call. ON CONFLICT (call_uuid) DO
// NOTHING makes the worker idempotent — re-processing the same recording won't
// duplicate or clobber. action_items is marshaled to JSONB (never NULL: a nil
// slice serializes to "[]").
func (s *Store) CreateCallInsight(ctx context.Context, tenantID *uuid.UUID, callUUID, transcript, summary string, actionItems []string) error {
	if actionItems == nil {
		actionItems = []string{}
	}
	itemsJSON, err := json.Marshal(actionItems)
	if err != nil {
		return err
	}
	const q = `
		INSERT INTO call_insights (tenant_id, call_uuid, transcript, summary, action_items)
		VALUES ($1, $2, NULLIF($3,''), NULLIF($4,''), $5)
		ON CONFLICT (call_uuid) DO NOTHING`
	_, err = s.DB.Exec(ctx, q, tenantID, callUUID, transcript, summary, itemsJSON)
	return err
}

// GetCallInsightByCallUUID fetches one insight, tenant-scoped. NULL transcript/
// summary are COALESCE'd to ” and action_items is JSONB (NOT NULL) decoded
// into the slice — NULL-safe throughout.
func (s *Store) GetCallInsightByCallUUID(ctx context.Context, tenantID uuid.UUID, callUUID string) (*CallInsight, error) {
	const q = `
		SELECT id, tenant_id, call_uuid,
		       COALESCE(transcript,''), COALESCE(summary,''),
		       action_items, created_at
		  FROM call_insights
		 WHERE call_uuid = $1 AND tenant_id = $2`
	var ci CallInsight
	var itemsJSON []byte
	err := s.DB.QueryRow(ctx, q, callUUID, tenantID).Scan(
		&ci.ID, &ci.TenantID, &ci.CallUUID,
		&ci.Transcript, &ci.Summary, &itemsJSON, &ci.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(itemsJSON) > 0 {
		_ = json.Unmarshal(itemsJSON, &ci.ActionItems)
	}
	if ci.ActionItems == nil {
		ci.ActionItems = []string{}
	}
	return &ci, nil
}

// GetCallInsightForCDR fetches the insight for a CDR by id, resolving the CDR's
// call_uuid + tenant in one trip. Returns the (possibly nil) insight; nil +
// pgx.ErrNoRows when the CDR has no insight row yet. Used by the API endpoint.
func (s *Store) GetCallInsightForCDR(ctx context.Context, tenantID, cdrID uuid.UUID) (*CallInsight, error) {
	const q = `
		SELECT ci.id, ci.tenant_id, ci.call_uuid,
		       COALESCE(ci.transcript,''), COALESCE(ci.summary,''),
		       ci.action_items, ci.created_at
		  FROM cdrs c
		  JOIN call_insights ci ON ci.call_uuid = c.call_uuid
		 WHERE c.id = $1 AND c.tenant_id = $2`
	var ci CallInsight
	var itemsJSON []byte
	err := s.DB.QueryRow(ctx, q, cdrID, tenantID).Scan(
		&ci.ID, &ci.TenantID, &ci.CallUUID,
		&ci.Transcript, &ci.Summary, &itemsJSON, &ci.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(itemsJSON) > 0 {
		_ = json.Unmarshal(itemsJSON, &ci.ActionItems)
	}
	if ci.ActionItems == nil {
		ci.ActionItems = []string{}
	}
	return &ci, nil
}

// ListCallInsightsByCDRForTenant returns a map of CDR id → CallInsight for a
// tenant, joining cdrs to call_insights on call_uuid. The portal CDR list uses
// it to annotate rows in one query instead of N+1 lookups. NULL-safe: summary/
// transcript are COALESCE'd, action_items is JSONB (NOT NULL).
func (s *Store) ListCallInsightsByCDRForTenant(ctx context.Context, tenantID uuid.UUID, limit int) (map[uuid.UUID]CallInsight, error) {
	if limit <= 0 || limit > 10000 {
		limit = 200
	}
	const q = `
		SELECT c.id,
		       ci.id, ci.tenant_id, ci.call_uuid,
		       COALESCE(ci.transcript,''), COALESCE(ci.summary,''),
		       ci.action_items, ci.created_at
		  FROM cdrs c
		  JOIN call_insights ci ON ci.call_uuid = c.call_uuid
		 WHERE c.tenant_id = $1
		 ORDER BY c.started_at DESC
		 LIMIT $2`
	rows, err := s.DB.Query(ctx, q, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]CallInsight{}
	for rows.Next() {
		var cdrID uuid.UUID
		var ci CallInsight
		var itemsJSON []byte
		if err := rows.Scan(
			&cdrID,
			&ci.ID, &ci.TenantID, &ci.CallUUID,
			&ci.Transcript, &ci.Summary, &itemsJSON, &ci.CreatedAt,
		); err != nil {
			return nil, err
		}
		if len(itemsJSON) > 0 {
			_ = json.Unmarshal(itemsJSON, &ci.ActionItems)
		}
		if ci.ActionItems == nil {
			ci.ActionItems = []string{}
		}
		out[cdrID] = ci
	}
	return out, rows.Err()
}

// PendingInsightCDR is a CDR awaiting transcription/summary: it has a recording
// on disk and no call_insights row yet.
type PendingInsightCDR struct {
	ID            uuid.UUID
	TenantID      *uuid.UUID
	CallUUID      string
	RecordingPath string
}

// ListCDRsPendingInsight returns CDRs that have a recording_path and no
// matching call_insights row, newest first. NULL-safe: recording_path is
// filtered with `<> ”` after COALESCE and only non-empty paths are selected;
// the NOT EXISTS anti-join avoids scanning any NULL into a non-nullable field.
// tenantID scopes the query (the worker iterates tenants).
func (s *Store) ListCDRsPendingInsight(ctx context.Context, tenantID uuid.UUID, limit int) ([]PendingInsightCDR, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	const q = `
		SELECT c.id, c.tenant_id, c.call_uuid, c.recording_path
		  FROM cdrs c
		 WHERE c.tenant_id = $1
		   AND c.recording_path IS NOT NULL AND c.recording_path <> ''
		   AND NOT EXISTS (
		       SELECT 1 FROM call_insights ci WHERE ci.call_uuid = c.call_uuid
		   )
		 ORDER BY c.started_at DESC
		 LIMIT $2`
	rows, err := s.DB.Query(ctx, q, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingInsightCDR
	for rows.Next() {
		var p PendingInsightCDR
		// recording_path is guaranteed non-NULL by the WHERE clause, but scan
		// into a plain string is still safe.
		if err := rows.Scan(&p.ID, &p.TenantID, &p.CallUUID, &p.RecordingPath); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListTenantIDsWithPendingInsight returns the distinct tenant ids that have at
// least one recording awaiting an insight. Lets the worker scope per-tenant
// queries without a separate tenant list. tenant_id NULL CDRs are excluded
// (insights are tenant-scoped).
func (s *Store) ListTenantIDsWithPendingInsight(ctx context.Context, limit int) ([]uuid.UUID, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	const q = `
		SELECT DISTINCT c.tenant_id
		  FROM cdrs c
		 WHERE c.tenant_id IS NOT NULL
		   AND c.recording_path IS NOT NULL AND c.recording_path <> ''
		   AND NOT EXISTS (
		       SELECT 1 FROM call_insights ci WHERE ci.call_uuid = c.call_uuid
		   )
		 LIMIT $1`
	rows, err := s.DB.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
