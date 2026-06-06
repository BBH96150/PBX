package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// DayCount is one day's call total (Day is YYYY-MM-DD, UTC).
type DayCount struct {
	Day   string
	Count int
}

// CallReport is the aggregate analytics for one tenant over a time window.
type CallReport struct {
	Total      int
	Answered   int
	AvgTalkSec int // average billable seconds of answered calls
	Inbound    int
	Outbound   int
	Internal   int
	PerDay     []DayCount
	PerHour    [24]int // calls bucketed by hour-of-day (UTC)
}

// GetCallReport aggregates the cdrs table for a tenant over [since, until).
func (s *Store) GetCallReport(ctx context.Context, tenantID uuid.UUID, since, until time.Time) (CallReport, error) {
	var r CallReport
	const sumQ = `
		SELECT count(*),
		       count(*) FILTER (WHERE disposition = 'ANSWERED'),
		       COALESCE(round(avg(billable_sec) FILTER (WHERE disposition = 'ANSWERED'))::int, 0),
		       count(*) FILTER (WHERE direction = 'inbound'),
		       count(*) FILTER (WHERE direction = 'outbound'),
		       count(*) FILTER (WHERE direction = 'internal')
		  FROM cdrs
		 WHERE tenant_id = $1 AND started_at >= $2 AND started_at < $3`
	if err := s.DB.QueryRow(ctx, sumQ, tenantID, since, until).Scan(
		&r.Total, &r.Answered, &r.AvgTalkSec, &r.Inbound, &r.Outbound, &r.Internal,
	); err != nil {
		return r, err
	}

	dayRows, err := s.DB.Query(ctx, `
		SELECT to_char(date_trunc('day', started_at), 'YYYY-MM-DD'), count(*)
		  FROM cdrs
		 WHERE tenant_id = $1 AND started_at >= $2 AND started_at < $3
		 GROUP BY 1 ORDER BY 1`, tenantID, since, until)
	if err != nil {
		return r, err
	}
	defer dayRows.Close()
	for dayRows.Next() {
		var d DayCount
		if err := dayRows.Scan(&d.Day, &d.Count); err != nil {
			return r, err
		}
		r.PerDay = append(r.PerDay, d)
	}
	if err := dayRows.Err(); err != nil {
		return r, err
	}

	hourRows, err := s.DB.Query(ctx, `
		SELECT extract(hour from started_at)::int, count(*)
		  FROM cdrs
		 WHERE tenant_id = $1 AND started_at >= $2 AND started_at < $3
		 GROUP BY 1`, tenantID, since, until)
	if err != nil {
		return r, err
	}
	defer hourRows.Close()
	for hourRows.Next() {
		var h, c int
		if err := hourRows.Scan(&h, &c); err != nil {
			return r, err
		}
		if h >= 0 && h < 24 {
			r.PerHour[h] = c
		}
	}
	return r, hourRows.Err()
}
