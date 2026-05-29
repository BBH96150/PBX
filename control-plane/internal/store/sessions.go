package store

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// Portal sessions are API tokens whose Name follows "portal:<email>:<suffix>".
// These helpers narrow the api_tokens table to that subset so the portal can
// show a per-user "active sessions" list and offer a "sign out everywhere"
// button.

// ListPortalSessionsForUser returns active portal sessions for an email.
// Excludes any token whose name doesn't match the portal: prefix (those are
// hand-issued API tokens that don't belong to a person).
func (s *Store) ListPortalSessionsForUser(ctx context.Context, email string) ([]APIToken, error) {
	prefix := "portal:" + strings.ToLower(email) + ":"
	const q = `
		SELECT id, tenant_id, name, token_prefix, scope, expires_at, last_used_at, created_at
		  FROM api_tokens
		 WHERE lower(name) LIKE $1 || '%'
		 ORDER BY COALESCE(last_used_at, created_at) DESC`
	rows, err := s.DB.Query(ctx, q, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(
			&t.ID, &t.TenantID, &t.Name, &t.TokenPrefix, &t.Scope,
			&t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokeAllPortalSessionsForUser nukes every portal session for an email,
// optionally preserving one (the caller's current session, so they don't
// log themselves out). Returns the count revoked.
func (s *Store) RevokeAllPortalSessionsForUser(ctx context.Context, email string, exceptID *uuid.UUID) (int, error) {
	prefix := "portal:" + strings.ToLower(email) + ":"
	args := []any{prefix}
	q := `DELETE FROM api_tokens WHERE lower(name) LIKE $1 || '%'`
	if exceptID != nil {
		q += ` AND id <> $2`
		args = append(args, *exceptID)
	}
	tag, err := s.DB.Exec(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
