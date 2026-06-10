package store

import (
	"context"

	"github.com/google/uuid"
)

// Registration is one live SIP registration from Kamailio's usrloc `location`
// table (populated once usrloc runs in db_mode). username is the AOR user part
// (the extension number) and domain is the tenant SIP domain.
type Registration struct {
	Username string
	Domain   string
}

// ActiveRegistrations returns the current SIP registrations from Kamailio's
// location table. With usrloc db_mode=1, Kamailio writes contacts here on
// REGISTER and deletes them on expiry, so a row present means the AOR is
// (recently) online. Returns an error if the table is absent — callers should
// degrade gracefully (presence simply shows as unavailable).
//
// We intentionally don't filter on `expires` here: the column's timezone
// semantics depend on the Kamailio postgres build, and Kamailio's own timer
// reaps expired rows. Trust that cleanup for now.
func (s *Store) ActiveRegistrations(ctx context.Context) ([]Registration, error) {
	const q = `SELECT username, COALESCE(domain,'') FROM location`
	rows, err := s.DB.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Registration
	for rows.Next() {
		var r Registration
		if err := rows.Scan(&r.Username, &r.Domain); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ExtensionPresence is one extension's live registration status, derived from
// whether a matching row exists in Kamailio's usrloc `location` table. A row
// present means the AOR is (recently) registered, i.e. "online" (see
// ActiveRegistrations' doc for the db_mode semantics).
type ExtensionPresence struct {
	ExtensionID uuid.UUID `json:"extension_id"`
	Extension   string    `json:"extension"`
	SIPUsername string    `json:"sip_username"`
	DisplayName string    `json:"display_name"`
	// Status is "online" when a location row exists for the extension's
	// (sip_username, domain), else "offline".
	Status string `json:"status"`
}

// ListExtensionPresenceForTenant returns the registration status of every
// active extension in a tenant. An extension is "online" iff a `location` row
// exists where location.username = e.sip_username AND location.domain =
// sd.domain; otherwise "offline".
//
// An EXISTS subquery (not a JOIN) keeps it NULL-safe and one-row-per-extension
// even with multiple registrations: no match → "offline" (never a NULL scanned
// into a non-nullable Go string). Ordered by extension number.
func (s *Store) ListExtensionPresenceForTenant(ctx context.Context, tenantID uuid.UUID) ([]ExtensionPresence, error) {
	const q = `
		SELECT e.id,
		       e.extension,
		       e.sip_username,
		       COALESCE(e.display_name, ''),
		       CASE WHEN EXISTS (
		           SELECT 1 FROM location l
		            WHERE l.username = e.sip_username AND l.domain = sd.domain
		       ) THEN 'online' ELSE 'offline' END AS status
		  FROM extensions e
		  JOIN sip_domains sd ON sd.id = e.sip_domain_id
		 WHERE e.tenant_id = $1 AND e.status = 'active'
		 ORDER BY e.extension`
	rows, err := s.DB.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExtensionPresence
	for rows.Next() {
		var p ExtensionPresence
		if err := rows.Scan(&p.ExtensionID, &p.Extension, &p.SIPUsername, &p.DisplayName, &p.Status); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
