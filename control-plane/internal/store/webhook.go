package store

import (
	"context"

	"github.com/google/uuid"
)

// WebhookEndpoint is a tenant-configured outbound webhook target.
type WebhookEndpoint struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	URL      string
	Secret   string
	Events   []string // subscribed event types; empty = all
	Enabled  bool
}

// CreateWebhookEndpoint inserts a new endpoint.
func (s *Store) CreateWebhookEndpoint(ctx context.Context, tenantID uuid.UUID, url, secret string, events []string) (*WebhookEndpoint, error) {
	// Never pass a nil slice — the column is NOT NULL and a nil would encode as
	// SQL NULL. An empty array means "all events".
	if events == nil {
		events = []string{}
	}
	const q = `
		INSERT INTO webhook_endpoints (tenant_id, url, secret, events)
		VALUES ($1, $2, $3, $4)
		RETURNING id, tenant_id, url, secret, events, enabled`
	var e WebhookEndpoint
	if err := s.DB.QueryRow(ctx, q, tenantID, url, secret, events).Scan(
		&e.ID, &e.TenantID, &e.URL, &e.Secret, &e.Events, &e.Enabled,
	); err != nil {
		return nil, err
	}
	return &e, nil
}

// ListWebhookEndpointsForTenant returns all endpoints for a tenant.
func (s *Store) ListWebhookEndpointsForTenant(ctx context.Context, tenantID uuid.UUID) ([]WebhookEndpoint, error) {
	const q = `
		SELECT id, tenant_id, url, secret, events, enabled
		  FROM webhook_endpoints WHERE tenant_id = $1 ORDER BY created_at`
	return s.scanWebhooks(ctx, q, tenantID)
}

// ListEnabledWebhookEndpointsForEvent returns the enabled endpoints in a tenant
// that are subscribed to the given event (empty events array = all events).
func (s *Store) ListEnabledWebhookEndpointsForEvent(ctx context.Context, tenantID uuid.UUID, event string) ([]WebhookEndpoint, error) {
	const q = `
		SELECT id, tenant_id, url, secret, events, enabled
		  FROM webhook_endpoints
		 WHERE tenant_id = $1 AND enabled = true
		   AND (cardinality(events) = 0 OR $2 = ANY(events))`
	return s.scanWebhooks(ctx, q, tenantID, event)
}

// GetWebhookEndpointForTenant fetches one endpoint scoped to its tenant.
func (s *Store) GetWebhookEndpointForTenant(ctx context.Context, tenantID, id uuid.UUID) (*WebhookEndpoint, error) {
	const q = `
		SELECT id, tenant_id, url, secret, events, enabled
		  FROM webhook_endpoints WHERE tenant_id = $1 AND id = $2`
	var e WebhookEndpoint
	if err := s.DB.QueryRow(ctx, q, tenantID, id).Scan(
		&e.ID, &e.TenantID, &e.URL, &e.Secret, &e.Events, &e.Enabled,
	); err != nil {
		return nil, err
	}
	return &e, nil
}

// DeleteWebhookEndpointForTenant removes an endpoint.
func (s *Store) DeleteWebhookEndpointForTenant(ctx context.Context, tenantID, id uuid.UUID) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM webhook_endpoints WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	return err
}

func (s *Store) scanWebhooks(ctx context.Context, q string, args ...any) ([]WebhookEndpoint, error) {
	rows, err := s.DB.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebhookEndpoint
	for rows.Next() {
		var e WebhookEndpoint
		if err := rows.Scan(&e.ID, &e.TenantID, &e.URL, &e.Secret, &e.Events, &e.Enabled); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
