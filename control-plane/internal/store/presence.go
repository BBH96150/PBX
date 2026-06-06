package store

import "context"

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
