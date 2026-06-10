package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// QueueCallback is a "keep your place in line" callback request (migration
// 0039). Instead of holding in a queue, a caller asks to be called back; the
// row records who to call and for which queue. A portal "Call now" action (or
// the background dialer) later originates the caller and transfers the answered
// leg into the queue so they reach the next agent.
//
// status lifecycle: pending → dialing → connected (success) | failed (retry up
// to attempt cap) ; cancelled is a terminal manual stop.
type QueueCallback struct {
	ID            uuid.UUID  `json:"id"`
	TenantID      uuid.UUID  `json:"tenant_id"`
	QueueID       uuid.UUID  `json:"queue_id"`
	CallerNumber  string     `json:"caller_number"`
	CallerName    string     `json:"caller_name,omitempty"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts"`
	RequestedAt   time.Time  `json:"requested_at"`
	LastAttemptAt *time.Time `json:"last_attempt_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

	// Joined convenience field populated by list queries: the target queue's
	// dialable extension (so the portal/worker can transfer into it without a
	// second lookup). May be empty if the queue has no extension.
	QueueExtension string `json:"queue_extension,omitempty"`
}

// queueCallbackCols is the canonical SELECT column list. last_attempt_at is the
// only nullable column — it is scanned into a *time.Time so a NULL doesn't break
// the scan (the exact LEFT-JOIN/NULL gotcha called out for this feature). All
// other columns are NOT NULL in the schema; caller_name is COALESCE'd to ''.
const queueCallbackCols = `
	id, tenant_id, queue_id, caller_number, COALESCE(caller_name,''),
	status, attempts, requested_at, last_attempt_at, created_at, updated_at`

type queueCallbackRow interface {
	Scan(dest ...any) error
}

func scanQueueCallback(r queueCallbackRow) (*QueueCallback, error) {
	var c QueueCallback
	if err := r.Scan(
		&c.ID, &c.TenantID, &c.QueueID, &c.CallerNumber, &c.CallerName,
		&c.Status, &c.Attempts, &c.RequestedAt, &c.LastAttemptAt, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &c, nil
}

// CreateQueueCallback inserts a callback request. The queue must belong to the
// tenant — enforced via a subquery so a cross-tenant queue_id can't be linked
// (a NULL subquery result violates the NOT NULL queue_id and surfaces an error).
func (s *Store) CreateQueueCallback(ctx context.Context, tenantID, queueID uuid.UUID, callerNumber, callerName string) (*QueueCallback, error) {
	const q = `
		INSERT INTO queue_callbacks (tenant_id, queue_id, caller_number, caller_name)
		VALUES (
		    $1,
		    (SELECT id FROM queues WHERE id = $2 AND tenant_id = $1),
		    $3, NULLIF($4,'')
		)
		RETURNING ` + queueCallbackCols
	return scanQueueCallback(s.DB.QueryRow(ctx, q, tenantID, queueID, callerNumber, callerName))
}

// RequestQueueCallback is the dialplan-facing create helper: a caller in (or
// destined for) a queue opts into a callback. Thin wrapper over
// CreateQueueCallback so the dialplan reads intent-first.
func (s *Store) RequestQueueCallback(ctx context.Context, tenantID, queueID uuid.UUID, callerNumber, callerName string) (*QueueCallback, error) {
	return s.CreateQueueCallback(ctx, tenantID, queueID, callerNumber, callerName)
}

// ListQueueCallbacksForTenant returns a tenant's callbacks, newest request
// first. When status is non-empty it filters to that status.
func (s *Store) ListQueueCallbacksForTenant(ctx context.Context, tenantID uuid.UUID, status string) ([]QueueCallback, error) {
	q := `
		SELECT qc.id, qc.tenant_id, qc.queue_id, qc.caller_number, COALESCE(qc.caller_name,''),
		       qc.status, qc.attempts, qc.requested_at, qc.last_attempt_at,
		       qc.created_at, qc.updated_at, COALESCE(qq.extension,'')
		  FROM queue_callbacks qc
		  JOIN queues qq ON qq.id = qc.queue_id
		 WHERE qc.tenant_id = $1`
	args := []any{tenantID}
	if status != "" {
		q += ` AND qc.status = $2`
		args = append(args, status)
	}
	q += ` ORDER BY qc.requested_at DESC`

	rows, err := s.DB.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QueueCallback
	for rows.Next() {
		var c QueueCallback
		if err := rows.Scan(
			&c.ID, &c.TenantID, &c.QueueID, &c.CallerNumber, &c.CallerName,
			&c.Status, &c.Attempts, &c.RequestedAt, &c.LastAttemptAt,
			&c.CreatedAt, &c.UpdatedAt, &c.QueueExtension,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListPendingQueueCallbacks returns a tenant's 'pending' callbacks (oldest
// first — FIFO so the longest-waiting caller is dialed back first). Used by the
// background dialer to pick the next callback to attempt.
func (s *Store) ListPendingQueueCallbacks(ctx context.Context, tenantID uuid.UUID) ([]QueueCallback, error) {
	const q = `
		SELECT qc.id, qc.tenant_id, qc.queue_id, qc.caller_number, COALESCE(qc.caller_name,''),
		       qc.status, qc.attempts, qc.requested_at, qc.last_attempt_at,
		       qc.created_at, qc.updated_at, COALESCE(qq.extension,'')
		  FROM queue_callbacks qc
		  JOIN queues qq ON qq.id = qc.queue_id
		 WHERE qc.tenant_id = $1 AND qc.status = 'pending'
		 ORDER BY qc.requested_at ASC`
	rows, err := s.DB.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QueueCallback
	for rows.Next() {
		var c QueueCallback
		if err := rows.Scan(
			&c.ID, &c.TenantID, &c.QueueID, &c.CallerNumber, &c.CallerName,
			&c.Status, &c.Attempts, &c.RequestedAt, &c.LastAttemptAt,
			&c.CreatedAt, &c.UpdatedAt, &c.QueueExtension,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListAllPendingQueueCallbacks returns every tenant's 'pending' callbacks
// (oldest first). Used by the platform-wide background dialer which has no
// single tenant context.
func (s *Store) ListAllPendingQueueCallbacks(ctx context.Context) ([]QueueCallback, error) {
	const q = `
		SELECT qc.id, qc.tenant_id, qc.queue_id, qc.caller_number, COALESCE(qc.caller_name,''),
		       qc.status, qc.attempts, qc.requested_at, qc.last_attempt_at,
		       qc.created_at, qc.updated_at, COALESCE(qq.extension,'')
		  FROM queue_callbacks qc
		  JOIN queues qq ON qq.id = qc.queue_id
		 WHERE qc.status = 'pending'
		 ORDER BY qc.requested_at ASC`
	rows, err := s.DB.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QueueCallback
	for rows.Next() {
		var c QueueCallback
		if err := rows.Scan(
			&c.ID, &c.TenantID, &c.QueueID, &c.CallerNumber, &c.CallerName,
			&c.Status, &c.Attempts, &c.RequestedAt, &c.LastAttemptAt,
			&c.CreatedAt, &c.UpdatedAt, &c.QueueExtension,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetQueueCallbackForTenant fetches one callback, enforcing tenant ownership.
// Returns pgx.ErrNoRows if not found for that tenant.
func (s *Store) GetQueueCallbackForTenant(ctx context.Context, tenantID, id uuid.UUID) (*QueueCallback, error) {
	const q = `
		SELECT qc.id, qc.tenant_id, qc.queue_id, qc.caller_number, COALESCE(qc.caller_name,''),
		       qc.status, qc.attempts, qc.requested_at, qc.last_attempt_at,
		       qc.created_at, qc.updated_at, COALESCE(qq.extension,'')
		  FROM queue_callbacks qc
		  JOIN queues qq ON qq.id = qc.queue_id
		 WHERE qc.id = $1 AND qc.tenant_id = $2`
	var c QueueCallback
	err := s.DB.QueryRow(ctx, q, id, tenantID).Scan(
		&c.ID, &c.TenantID, &c.QueueID, &c.CallerNumber, &c.CallerName,
		&c.Status, &c.Attempts, &c.RequestedAt, &c.LastAttemptAt,
		&c.CreatedAt, &c.UpdatedAt, &c.QueueExtension,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// SetQueueCallbackStatus updates a callback's status, tenant-scoped. Returns
// ErrCrossTenant if no row matched (wrong tenant or unknown id).
func (s *Store) SetQueueCallbackStatus(ctx context.Context, tenantID, id uuid.UUID, status string) error {
	const q = `
		UPDATE queue_callbacks SET status = $3, updated_at = now()
		 WHERE id = $1 AND tenant_id = $2`
	ct, err := s.DB.Exec(ctx, q, id, tenantID, status)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrCrossTenant
	}
	return nil
}

// CancelQueueCallback marks a callback 'cancelled' (a manual terminal stop),
// tenant-scoped.
func (s *Store) CancelQueueCallback(ctx context.Context, tenantID, id uuid.UUID) error {
	return s.SetQueueCallbackStatus(ctx, tenantID, id, "cancelled")
}

// IncrementQueueCallbackAttempt bumps the attempt counter, stamps
// last_attempt_at, and sets the status in one round-trip (the dialer marks a row
// 'dialing' as it picks it up). Tenant-scoped.
func (s *Store) IncrementQueueCallbackAttempt(ctx context.Context, tenantID, id uuid.UUID, status string) error {
	const q = `
		UPDATE queue_callbacks
		   SET attempts = attempts + 1,
		       last_attempt_at = now(),
		       status = $3,
		       updated_at = now()
		 WHERE id = $1 AND tenant_id = $2`
	ct, err := s.DB.Exec(ctx, q, id, tenantID, status)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrCrossTenant
	}
	return nil
}
