package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// SpeedDial is a single personal speed-dial / favorite owned by one end-user
// (see migration 0044). It is per-USER, not per-tenant: every query is keyed on
// user_id. tenant_id is carried for integrity / RLS-style scoping only. The CALL
// button on the self-service page dials Number via the existing click-to-dial
// originate path; nothing here touches the dialplan.
type SpeedDial struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	Label     string    `json:"label"`
	Number    string    `json:"number"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateSpeedDialInput struct {
	UserID    uuid.UUID
	TenantID  uuid.UUID
	Label     string
	Number    string
	SortOrder int
}

// CreateSpeedDial inserts a speed dial for the owning user. Caller derives
// UserID + TenantID from the session (never from request input) and is expected
// to have already sanitized/validated Number.
func (s *Store) CreateSpeedDial(ctx context.Context, in CreateSpeedDialInput) (*SpeedDial, error) {
	const q = `
		INSERT INTO speed_dials (user_id, tenant_id, label, number, sort_order)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, user_id, tenant_id, label, number, sort_order, created_at`
	var d SpeedDial
	err := s.DB.QueryRow(ctx, q, in.UserID, in.TenantID, in.Label, in.Number, in.SortOrder).Scan(
		&d.ID, &d.UserID, &d.TenantID, &d.Label, &d.Number, &d.SortOrder, &d.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ListSpeedDialsForUser returns a user's own speed dials, ordered for display.
func (s *Store) ListSpeedDialsForUser(ctx context.Context, userID uuid.UUID) ([]SpeedDial, error) {
	const q = `
		SELECT id, user_id, tenant_id, label, number, sort_order, created_at
		  FROM speed_dials WHERE user_id = $1
		 ORDER BY sort_order, created_at`
	rows, err := s.DB.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SpeedDial
	for rows.Next() {
		var d SpeedDial
		if err := rows.Scan(
			&d.ID, &d.UserID, &d.TenantID, &d.Label, &d.Number, &d.SortOrder, &d.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeleteSpeedDialForUser removes one of the user's own speed dials. The WHERE
// clause is keyed on BOTH id AND user_id, so a user can never delete another
// user's entry — a non-match (wrong owner or unknown id) returns ErrCrossTenant
// rather than silently succeeding.
func (s *Store) DeleteSpeedDialForUser(ctx context.Context, userID, id uuid.UUID) error {
	const q = `DELETE FROM speed_dials WHERE id = $1 AND user_id = $2`
	ct, err := s.DB.Exec(ctx, q, id, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrCrossTenant
	}
	return nil
}
