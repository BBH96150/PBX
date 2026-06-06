package portal

import (
	"fmt"
	"net/http"
	"time"
)

// barView is one bar in a simple CSS bar chart: Pct is 0–100 of the tallest bar.
type barView struct {
	Label string
	Count int
	Pct   int
}

func (s *Server) reportsView(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	days := 30
	switch r.URL.Query().Get("days") {
	case "7":
		days = 7
	case "90":
		days = 90
	}
	now := time.Now().UTC()
	until := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	since := until.AddDate(0, 0, -days)

	rep, err := s.store.GetCallReport(r.Context(), tid, since, until)
	if err != nil {
		s.errPage(w, r, err)
		return
	}

	answerRate := 0
	if rep.Total > 0 {
		answerRate = rep.Answered * 100 / rep.Total
	}

	maxDay := 1
	for _, d := range rep.PerDay {
		if d.Count > maxDay {
			maxDay = d.Count
		}
	}
	dayBars := make([]barView, 0, len(rep.PerDay))
	for _, d := range rep.PerDay {
		dayBars = append(dayBars, barView{Label: d.Day, Count: d.Count, Pct: d.Count * 100 / maxDay})
	}

	maxHour := 1
	for _, c := range rep.PerHour {
		if c > maxHour {
			maxHour = c
		}
	}
	hourBars := make([]barView, 0, 24)
	for h := 0; h < 24; h++ {
		hourBars = append(hourBars, barView{Label: fmt.Sprintf("%02d", h), Count: rep.PerHour[h], Pct: rep.PerHour[h] * 100 / maxHour})
	}

	dayFrom, dayTo := "", ""
	if len(dayBars) > 0 {
		dayFrom = dayBars[0].Label
		dayTo = dayBars[len(dayBars)-1].Label
	}

	s.renderLayout(w, r, tenant.Name+" · Reports", "reports", map[string]any{
		"Tenant":     tenant,
		"NavActive":  "reports",
		"Report":     rep,
		"AnswerRate": answerRate,
		"DayBars":    dayBars,
		"HourBars":   hourBars,
		"Days":       days,
		"DayFrom":    dayFrom,
		"DayTo":      dayTo,
	})
}
