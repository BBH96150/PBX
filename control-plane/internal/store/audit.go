package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type AuditEntry struct {
	ID           uuid.UUID       `json:"id"`
	TenantID     *uuid.UUID      `json:"tenant_id,omitempty"`
	ActorUserID  *uuid.UUID      `json:"actor_user_id,omitempty"`
	ActorTokenID *uuid.UUID      `json:"actor_token_id,omitempty"`
	ActorEmail   string          `json:"actor_email,omitempty"`
	Event        string          `json:"event"`
	TargetType   string          `json:"target_type,omitempty"`
	TargetID     *uuid.UUID      `json:"target_id,omitempty"`
	IPAddress    string          `json:"ip_address,omitempty"`
	UserAgent    string          `json:"user_agent,omitempty"`
	Payload      json.RawMessage `json:"payload"`
	CreatedAt    time.Time       `json:"created_at"`
}

// ListAuditForTenant returns the most-recent `limit` events for a tenant.
// Pass uuid.Nil to fetch platform-level events (tenant_id IS NULL).
func (s *Store) ListAuditForTenant(ctx context.Context, tenantID uuid.UUID, limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = 200
	}
	var (
		q    string
		rows interface {
			Next() bool
			Close()
		}
	)
	if tenantID == uuid.Nil {
		q = `
		  SELECT id, tenant_id, actor_user_id, actor_token_id,
		         COALESCE(actor_email,''), event,
		         COALESCE(target_type,''), target_id,
		         COALESCE(host(ip_address),''), COALESCE(user_agent,''),
		         payload::text, created_at
		    FROM audit_log
		   WHERE tenant_id IS NULL
		   ORDER BY created_at DESC LIMIT $1`
		r, err := s.DB.Query(ctx, q, limit)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return scanAudit(r)
	}
	q = `
	  SELECT id, tenant_id, actor_user_id, actor_token_id,
	         COALESCE(actor_email,''), event,
	         COALESCE(target_type,''), target_id,
	         COALESCE(host(ip_address),''), COALESCE(user_agent,''),
	         payload::text, created_at
	    FROM audit_log
	   WHERE tenant_id = $1
	   ORDER BY created_at DESC LIMIT $2`
	r, err := s.DB.Query(ctx, q, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	_ = rows
	return scanAudit(r)
}

type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanAudit(r rowScanner) ([]AuditEntry, error) {
	var out []AuditEntry
	for r.Next() {
		var e AuditEntry
		var payload string
		if err := r.Scan(
			&e.ID, &e.TenantID, &e.ActorUserID, &e.ActorTokenID,
			&e.ActorEmail, &e.Event,
			&e.TargetType, &e.TargetID,
			&e.IPAddress, &e.UserAgent,
			&payload, &e.CreatedAt,
		); err != nil {
			return nil, err
		}
		e.Payload = json.RawMessage(payload)
		out = append(out, e)
	}
	return out, r.Err()
}

// MarkEmailVerified flips users.email_verified_at to now() if still NULL.
// Idempotent; returns whether the row was updated.
func (s *Store) MarkEmailVerified(ctx context.Context, userID uuid.UUID) (bool, error) {
	tag, err := s.DB.Exec(ctx,
		`UPDATE users SET email_verified_at = now() WHERE id = $1 AND email_verified_at IS NULL`,
		userID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// TenantRequiresEmailVerified reads the per-tenant gate.
func (s *Store) TenantRequiresEmailVerified(ctx context.Context, tenantID uuid.UUID) (bool, error) {
	var b bool
	err := s.DB.QueryRow(ctx, `SELECT require_email_verified FROM tenants WHERE id = $1`, tenantID).Scan(&b)
	return b, err
}
