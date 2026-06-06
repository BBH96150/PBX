package store

import (
	"context"

	"github.com/google/uuid"
)

// Contact is one entry in a tenant's shared directory.
type Contact struct {
	ID      uuid.UUID
	Name    string
	Number  string
	Company string
	Notes   string
}

// CreateContact adds a directory entry.
func (s *Store) CreateContact(ctx context.Context, tenantID uuid.UUID, name, number, company, notes string) (*Contact, error) {
	const q = `
		INSERT INTO contacts (tenant_id, name, number, company, notes)
		VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''))
		RETURNING id, name, number, COALESCE(company,''), COALESCE(notes,'')`
	var c Contact
	if err := s.DB.QueryRow(ctx, q, tenantID, name, number, company, notes).Scan(
		&c.ID, &c.Name, &c.Number, &c.Company, &c.Notes,
	); err != nil {
		return nil, err
	}
	return &c, nil
}

// ListContactsForTenant returns a tenant's contacts, optionally filtered by a
// case-insensitive substring across name/number/company.
func (s *Store) ListContactsForTenant(ctx context.Context, tenantID uuid.UUID, search string) ([]Contact, error) {
	const q = `
		SELECT id, name, number, COALESCE(company,''), COALESCE(notes,'')
		  FROM contacts
		 WHERE tenant_id = $1
		   AND ($2 = '' OR name ILIKE '%'||$2||'%' OR number ILIKE '%'||$2||'%'
		        OR COALESCE(company,'') ILIKE '%'||$2||'%')
		 ORDER BY name`
	rows, err := s.DB.Query(ctx, q, tenantID, search)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Contact
	for rows.Next() {
		var c Contact
		if err := rows.Scan(&c.ID, &c.Name, &c.Number, &c.Company, &c.Notes); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteContactForTenant removes a directory entry.
func (s *Store) DeleteContactForTenant(ctx context.Context, tenantID, id uuid.UUID) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM contacts WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	return err
}
