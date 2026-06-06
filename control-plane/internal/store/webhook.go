package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// WebhookEndpoint is a tenant-configured outbound webhook target.
type WebhookEndpoint struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	URL           string
	Secret        string
	Events        []string // subscribed event types; empty = all
	Enabled       bool
	LastStatus    *string    // 'ok' | 'fail' | nil (never delivered)
	LastError     *string    // last failure detail
	LastAttemptAt *time.Time // last delivery attempt
}

const webhookCols = `id, tenant_id, url, secret, events, enabled, last_status, last_error, last_attempt_at`

func scanWebhook(row interface{ Scan(...any) error }) (WebhookEndpoint, error) {
	var e WebhookEndpoint
	err := row.Scan(&e.ID, &e.TenantID, &e.URL, &e.Secret, &e.Events, &e.Enabled,
		&e.LastStatus, &e.LastError, &e.LastAttemptAt)
	return e, err
}

// CreateWebhookEndpoint inserts a new endpoint.
func (s *Store) CreateWebhookEndpoint(ctx context.Context, tenantID uuid.UUID, url, secret string, events []string) (*WebhookEndpoint, error) {
	// Never pass a nil slice — the column is NOT NULL and a nil would encode as
	// SQL NULL. An empty array means "all events".
	if events == nil {
		events = []string{}
	}
	q := `INSERT INTO webhook_endpoints (tenant_id, url, secret, events)
	      VALUES ($1, $2, $3, $4) RETURNING ` + webhookCols
	e, err := scanWebhook(s.DB.QueryRow(ctx, q, tenantID, url, secret, events))
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListWebhookEndpointsForTenant returns all endpoints for a tenant.
func (s *Store) ListWebhookEndpointsForTenant(ctx context.Context, tenantID uuid.UUID) ([]WebhookEndpoint, error) {
	return s.queryWebhooks(ctx, `SELECT `+webhookCols+` FROM webhook_endpoints WHERE tenant_id = $1 ORDER BY created_at`, tenantID)
}

// ListEnabledWebhookEndpointsForEvent returns the enabled endpoints in a tenant
// subscribed to the given event (empty events array = all events).
func (s *Store) ListEnabledWebhookEndpointsForEvent(ctx context.Context, tenantID uuid.UUID, event string) ([]WebhookEndpoint, error) {
	return s.queryWebhooks(ctx, `SELECT `+webhookCols+`
		  FROM webhook_endpoints
		 WHERE tenant_id = $1 AND enabled = true
		   AND (cardinality(events) = 0 OR $2 = ANY(events))`, tenantID, event)
}

// GetWebhookEndpointForTenant fetches one endpoint scoped to its tenant.
func (s *Store) GetWebhookEndpointForTenant(ctx context.Context, tenantID, id uuid.UUID) (*WebhookEndpoint, error) {
	e, err := scanWebhook(s.DB.QueryRow(ctx,
		`SELECT `+webhookCols+` FROM webhook_endpoints WHERE tenant_id = $1 AND id = $2`, tenantID, id))
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// DeleteWebhookEndpointForTenant removes an endpoint.
func (s *Store) DeleteWebhookEndpointForTenant(ctx context.Context, tenantID, id uuid.UUID) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM webhook_endpoints WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	return err
}

// SetWebhookEnabled enables/disables an endpoint (tenant-scoped).
func (s *Store) SetWebhookEnabled(ctx context.Context, tenantID, id uuid.UUID, enabled bool) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE webhook_endpoints SET enabled = $3, updated_at = now() WHERE tenant_id = $1 AND id = $2`,
		tenantID, id, enabled)
	return err
}

// RotateWebhookSecret replaces an endpoint's signing secret (tenant-scoped).
func (s *Store) RotateWebhookSecret(ctx context.Context, tenantID, id uuid.UUID, newSecret string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE webhook_endpoints SET secret = $3, updated_at = now() WHERE tenant_id = $1 AND id = $2`,
		tenantID, id, newSecret)
	return err
}

// RecordWebhookDelivery stores the most recent delivery outcome for an endpoint.
// status is "ok" or "fail"; errMsg is empty on success.
func (s *Store) RecordWebhookDelivery(ctx context.Context, id uuid.UUID, status, errMsg string) error {
	var ep *string
	if errMsg != "" {
		ep = &errMsg
	}
	_, err := s.DB.Exec(ctx,
		`UPDATE webhook_endpoints SET last_status = $2, last_error = $3, last_attempt_at = now() WHERE id = $1`,
		id, status, ep)
	return err
}

func (s *Store) queryWebhooks(ctx context.Context, q string, args ...any) ([]WebhookEndpoint, error) {
	rows, err := s.DB.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebhookEndpoint
	for rows.Next() {
		e, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
