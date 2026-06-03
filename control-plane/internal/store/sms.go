package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type SMSMessage struct {
	ID         uuid.UUID `json:"id"`
	TenantID   uuid.UUID `json:"tenant_id"`
	OurE164    string    `json:"our_e164"`
	PeerE164   string    `json:"peer_e164"`
	Direction  string    `json:"direction"`
	Body       string    `json:"body"`
	Status     string    `json:"status"`
	ProviderID string    `json:"provider_id,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type CreateSMSInput struct {
	TenantID  uuid.UUID
	OurE164   string
	PeerE164  string
	Direction string
	Body      string
	Status    string // default per direction
}

func (s *Store) CreateSMS(ctx context.Context, in CreateSMSInput) (*SMSMessage, error) {
	if in.Status == "" {
		if in.Direction == "inbound" {
			in.Status = "received"
		} else {
			in.Status = "queued"
		}
	}
	const q = `
		INSERT INTO sms_messages (tenant_id, our_e164, peer_e164, direction, body, status)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, tenant_id, our_e164, peer_e164, direction, body, status,
		          COALESCE(provider_id,''), COALESCE(error,''), created_at`
	var m SMSMessage
	err := s.DB.QueryRow(ctx, q, in.TenantID, in.OurE164, in.PeerE164, in.Direction, in.Body, in.Status).Scan(
		&m.ID, &m.TenantID, &m.OurE164, &m.PeerE164, &m.Direction, &m.Body, &m.Status,
		&m.ProviderID, &m.Error, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// SMSConversation summarises a thread (one peer ↔ one of our DIDs).
type SMSConversation struct {
	OurE164      string    `json:"our_e164"`
	PeerE164     string    `json:"peer_e164"`
	LastBody     string    `json:"last_body"`
	LastAt       time.Time `json:"last_at"`
	LastDir      string    `json:"last_dir"`
	MessageCount int       `json:"message_count"`
}

// ListSMSConversations returns the most-recent message per (our_e164, peer_e164)
// pair for a tenant, newest thread first.
func (s *Store) ListSMSConversations(ctx context.Context, tenantID uuid.UUID) ([]SMSConversation, error) {
	const q = `
		SELECT our_e164, peer_e164, last_body, last_at, last_dir, msg_count FROM (
			SELECT DISTINCT ON (our_e164, peer_e164)
			       our_e164, peer_e164, body AS last_body, created_at AS last_at,
			       direction AS last_dir,
			       count(*) OVER (PARTITION BY our_e164, peer_e164) AS msg_count
			  FROM sms_messages WHERE tenant_id = $1
			 ORDER BY our_e164, peer_e164, created_at DESC
		) t ORDER BY last_at DESC`
	rows, err := s.DB.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SMSConversation
	for rows.Next() {
		var c SMSConversation
		if err := rows.Scan(&c.OurE164, &c.PeerE164, &c.LastBody, &c.LastAt, &c.LastDir, &c.MessageCount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListSMSThread returns all messages between our_e164 and peer_e164, oldest first.
func (s *Store) ListSMSThread(ctx context.Context, tenantID uuid.UUID, ourE164, peerE164 string) ([]SMSMessage, error) {
	const q = `
		SELECT id, tenant_id, our_e164, peer_e164, direction, body, status,
		       COALESCE(provider_id,''), COALESCE(error,''), created_at
		  FROM sms_messages
		 WHERE tenant_id = $1 AND our_e164 = $2 AND peer_e164 = $3
		 ORDER BY created_at`
	rows, err := s.DB.Query(ctx, q, tenantID, ourE164, peerE164)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SMSMessage
	for rows.Next() {
		var m SMSMessage
		if err := rows.Scan(&m.ID, &m.TenantID, &m.OurE164, &m.PeerE164, &m.Direction, &m.Body,
			&m.Status, &m.ProviderID, &m.Error, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetSMSStatus updates delivery status after a send attempt.
func (s *Store) SetSMSStatus(ctx context.Context, id uuid.UUID, status, providerID, errMsg string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE sms_messages SET status=$2, provider_id=NULLIF($3,''), error=NULLIF($4,'') WHERE id=$1`,
		id, status, providerID, errMsg)
	return err
}

// TenantByDID resolves one of our DIDs (E.164) to its tenant — used by the
// inbound SMS webhook to route a received message.
func (s *Store) TenantIDByDID(ctx context.Context, e164 string) (uuid.UUID, error) {
	var tid uuid.UUID
	err := s.DB.QueryRow(ctx, `SELECT tenant_id FROM dids WHERE e164 = $1 LIMIT 1`, e164).Scan(&tid)
	return tid, err
}
