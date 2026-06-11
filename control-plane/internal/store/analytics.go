package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// LabelCount is a single (label, count) aggregate row — used for the top-callers
// and top-destinations leaderboards on the analytics dashboard.
type LabelCount struct {
	Label string
	Count int
}

// CallAnalytics is the read-only analytics rollup for one tenant over a window.
// It reuses CallReport for the headline/day/hour figures (so the math matches
// the Reports page exactly) and adds total talk time plus the two leaderboards.
type CallAnalytics struct {
	CallReport
	TotalTalkSec int          // sum of billable seconds across answered calls
	TopCallers   []LabelCount // top from/caller-id by call count
	TopDests     []LabelCount // top to/destination by call count
}

// GetCallAnalytics aggregates the cdrs table for a tenant over [since, until).
// Every query is tenant-scoped (WHERE tenant_id = $1) and the window is fully
// parameterized. Aggregation happens in SQL (GROUP BY) — no rows are pulled
// into Go.
func (s *Store) GetCallAnalytics(ctx context.Context, tenantID uuid.UUID, since, until time.Time) (CallAnalytics, error) {
	var a CallAnalytics

	// Reuse the canonical report aggregation for totals/answer/direction/day/hour
	// so "answer rate" and the bar charts are computed identically to /reports.
	rep, err := s.GetCallReport(ctx, tenantID, since, until)
	if err != nil {
		return a, err
	}
	a.CallReport = rep

	// Total talk time = sum of billable seconds on answered calls.
	const talkQ = `
		SELECT COALESCE(sum(billable_sec) FILTER (WHERE disposition = 'ANSWERED'), 0)::int
		  FROM cdrs
		 WHERE tenant_id = $1 AND started_at >= $2 AND started_at < $3`
	if err := s.DB.QueryRow(ctx, talkQ, tenantID, since, until).Scan(&a.TotalTalkSec); err != nil {
		return a, err
	}

	// Top 10 callers by caller-id number (falling back to the from URI when the
	// number is blank). NULLIF + COALESCE keep blank rows from collapsing into a
	// single empty bucket dominating the board.
	const callersQ = `
		SELECT COALESCE(NULLIF(caller_id_num, ''), from_uri) AS who, count(*)
		  FROM cdrs
		 WHERE tenant_id = $1 AND started_at >= $2 AND started_at < $3
		 GROUP BY who
		 ORDER BY count(*) DESC, who
		 LIMIT 10`
	a.TopCallers, err = s.scanLabelCounts(ctx, callersQ, tenantID, since, until)
	if err != nil {
		return a, err
	}

	// Top 10 destinations by the to URI.
	const destsQ = `
		SELECT to_uri AS dest, count(*)
		  FROM cdrs
		 WHERE tenant_id = $1 AND started_at >= $2 AND started_at < $3
		 GROUP BY dest
		 ORDER BY count(*) DESC, dest
		 LIMIT 10`
	a.TopDests, err = s.scanLabelCounts(ctx, destsQ, tenantID, since, until)
	if err != nil {
		return a, err
	}

	return a, nil
}

// scanLabelCounts runs a (label, count) GROUP BY query and collects the rows.
func (s *Store) scanLabelCounts(ctx context.Context, q string, args ...any) ([]LabelCount, error) {
	rows, err := s.DB.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LabelCount
	for rows.Next() {
		var lc LabelCount
		if err := rows.Scan(&lc.Label, &lc.Count); err != nil {
			return nil, err
		}
		out = append(out, lc)
	}
	return out, rows.Err()
}
