package portal

import (
	"fmt"
	"net/http"
	"time"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// Call analytics dashboard: a portal-only, read-only view that aggregates the
// existing cdrs table into headline KPIs, day/hour distributions, and top
// caller/destination leaderboards over a selectable window. No new data is
// collected and there is no /v1 route — all aggregation lives in SQL
// (store.GetCallAnalytics).

// labelBarView is one leaderboard row: Pct is 0–100 of the busiest row, used to
// draw an inline CSS bar behind the label.
type labelBarView struct {
	Label string
	Count int
	Pct   int
}

// pct returns part as an integer percentage of total, clamped to 0 when total
// is zero. Pure helper so it can be unit-tested independently of the handler.
func pct(part, total int) int {
	if total <= 0 {
		return 0
	}
	return part * 100 / total
}

// analyticsDays maps the ?days= query param to one of the three supported
// windows, defaulting to 30.
func analyticsDays(q string) int {
	switch q {
	case "7":
		return 7
	case "90":
		return 90
	default:
		return 30
	}
}

func (s *Server) tenantAnalytics(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	days := analyticsDays(r.URL.Query().Get("days"))
	now := time.Now().UTC()
	until := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	since := until.AddDate(0, 0, -days)

	an, err := s.store.GetCallAnalytics(r.Context(), tid, since, until)
	if err != nil {
		s.errPage(w, r, err)
		return
	}

	answerRate := pct(an.Answered, an.Total)

	maxDay := 1
	for _, d := range an.PerDay {
		if d.Count > maxDay {
			maxDay = d.Count
		}
	}
	dayBars := make([]labelBarView, 0, len(an.PerDay))
	for _, d := range an.PerDay {
		dayBars = append(dayBars, labelBarView{Label: d.Day, Count: d.Count, Pct: pct(d.Count, maxDay)})
	}

	maxHour := 1
	for _, c := range an.PerHour {
		if c > maxHour {
			maxHour = c
		}
	}
	hourBars := make([]labelBarView, 0, 24)
	for h := 0; h < 24; h++ {
		hourBars = append(hourBars, labelBarView{Label: fmt.Sprintf("%02d", h), Count: an.PerHour[h], Pct: pct(an.PerHour[h], maxHour)})
	}

	callerBars := leaderboard(an.TopCallers)
	destBars := leaderboard(an.TopDests)

	dayFrom, dayTo := "", ""
	if len(dayBars) > 0 {
		dayFrom = dayBars[0].Label
		dayTo = dayBars[len(dayBars)-1].Label
	}

	s.renderLayout(w, r, tenant.Name+" · Analytics", "analytics", map[string]any{
		"Tenant":     tenant,
		"NavActive":  "analytics",
		"Report":     an.CallReport,
		"AnswerRate": answerRate,
		"TotalTalk":  an.TotalTalkSec,
		"DayBars":    dayBars,
		"HourBars":   hourBars,
		"TopCallers": callerBars,
		"TopDests":   destBars,
		"Days":       days,
		"DayFrom":    dayFrom,
		"DayTo":      dayTo,
	})
}

// leaderboard turns top-N (label, count) rows into bar rows scaled to the
// busiest row in the set.
func leaderboard(rows []store.LabelCount) []labelBarView {
	max := 1
	for _, lc := range rows {
		if lc.Count > max {
			max = lc.Count
		}
	}
	out := make([]labelBarView, 0, len(rows))
	for _, lc := range rows {
		out = append(out, labelBarView{Label: lc.Label, Count: lc.Count, Pct: pct(lc.Count, max)})
	}
	return out
}
