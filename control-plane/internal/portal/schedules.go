package portal

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

var weekdayNames = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

// schedulePeriodView is a period decorated with its weekday name + HH:MM times.
type schedulePeriodView struct {
	ID       uuid.UUID
	Weekday  int
	WeekName string
	Open     string
	Close    string
}

func (s *Server) schedulesList(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	schedules, _ := s.store.ListSchedulesForTenant(r.Context(), tid)
	s.renderLayout(w, r, tenant.Name+" · Business hours", "schedules", map[string]any{
		"Tenant":    tenant,
		"Schedules": schedules,
		"NavActive": "schedules",
	})
}

func (s *Server) scheduleCreate(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	redirect := "/admin/tenants/" + tid.String() + "/schedules"
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		s.flashErr(w, r, redirect, errors.New("give the schedule a name"))
		return
	}
	sc, err := s.store.CreateSchedule(r.Context(), tid, name, strings.TrimSpace(r.FormValue("timezone")))
	if err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	http.Redirect(w, r, redirect+"/"+sc.ID.String(), http.StatusSeeOther)
}

func (s *Server) scheduleDetail(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	schedID, err := uuid.Parse(chi.URLParam(r, "schedID"))
	if err != nil {
		http.Error(w, "bad schedule id", 400)
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	sc, err := s.store.GetScheduleForTenant(r.Context(), tid, schedID)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	rawPeriods, _ := s.store.ListSchedulePeriods(r.Context(), schedID)
	periods := make([]schedulePeriodView, 0, len(rawPeriods))
	for _, p := range rawPeriods {
		periods = append(periods, schedulePeriodView{
			ID: p.ID, Weekday: p.Weekday, WeekName: weekdayNames[p.Weekday%7],
			Open: secToHM(p.OpenSec), Close: secToHM(p.CloseSec),
		})
	}
	holidays, _ := s.store.ListScheduleHolidays(r.Context(), schedID)

	s.renderLayout(w, r, tenant.Name+" · "+sc.Name, "schedule", map[string]any{
		"Tenant":   tenant,
		"Schedule": sc,
		"Periods":  periods,
		"Holidays": holidays,
		"Weekdays": weekdayNames,
	})
}

func (s *Server) scheduleDelete(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "schedID"))
	if err != nil {
		http.Error(w, "bad schedule id", 400)
		return
	}
	home := "/admin/tenants/" + tid.String() + "/schedules"
	if err := s.store.DeleteScheduleForTenant(r.Context(), tid, id); err != nil {
		s.flashErr(w, r, home+"/"+id.String(), err)
		return
	}
	http.Redirect(w, r, home+"?flash=Schedule+deleted.", http.StatusSeeOther)
}

func (s *Server) schedulePeriodAdd(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	schedID, err := uuid.Parse(chi.URLParam(r, "schedID"))
	if err != nil {
		http.Error(w, "bad schedule id", 400)
		return
	}
	_ = r.ParseForm()
	redirect := "/admin/tenants/" + tid.String() + "/schedules/" + schedID.String()
	wd := atoiOr(r.FormValue("weekday"), -1)
	if wd < 0 || wd > 6 {
		s.flashErr(w, r, redirect, errors.New("pick a weekday"))
		return
	}
	open := strings.TrimSpace(r.FormValue("open"))
	close := strings.TrimSpace(r.FormValue("close"))
	if open == "" || close == "" {
		s.flashErr(w, r, redirect, errors.New("set both open and close times"))
		return
	}
	if err := s.store.AddSchedulePeriod(r.Context(), tid, schedID, wd, open, close); err != nil {
		s.flashErr(w, r, redirect, friendlyDupErr(err, "couldn't add — check the times (close must be after open)"))
		return
	}
	http.Redirect(w, r, redirect+"?flash=Hours+added.", http.StatusSeeOther)
}

func (s *Server) schedulePeriodRemove(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	schedID := chi.URLParam(r, "schedID")
	pid, err := uuid.Parse(chi.URLParam(r, "periodID"))
	if err != nil {
		http.Error(w, "bad period id", 400)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/schedules/" + schedID
	if err := s.store.DeleteSchedulePeriodForTenant(r.Context(), tid, pid); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	http.Redirect(w, r, redirect+"?flash=Removed.", http.StatusSeeOther)
}

func (s *Server) scheduleHolidayAdd(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	schedID, err := uuid.Parse(chi.URLParam(r, "schedID"))
	if err != nil {
		http.Error(w, "bad schedule id", 400)
		return
	}
	_ = r.ParseForm()
	redirect := "/admin/tenants/" + tid.String() + "/schedules/" + schedID.String()
	date := strings.TrimSpace(r.FormValue("on_date"))
	if date == "" {
		s.flashErr(w, r, redirect, errors.New("pick a date"))
		return
	}
	if err := s.store.AddScheduleHoliday(r.Context(), tid, schedID, date,
		strings.TrimSpace(r.FormValue("name")), r.FormValue("is_open") == "true"); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	http.Redirect(w, r, redirect+"?flash=Date+override+saved.", http.StatusSeeOther)
}

func (s *Server) scheduleHolidayRemove(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	schedID := chi.URLParam(r, "schedID")
	hid, err := uuid.Parse(chi.URLParam(r, "holidayID"))
	if err != nil {
		http.Error(w, "bad holiday id", 400)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/schedules/" + schedID
	if err := s.store.DeleteScheduleHolidayForTenant(r.Context(), tid, hid); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	http.Redirect(w, r, redirect+"?flash=Removed.", http.StatusSeeOther)
}

// secToHM renders seconds-since-midnight as "HH:MM".
func secToHM(sec int) string {
	h := sec / 3600
	m := (sec % 3600) / 60
	hh := []byte{byte('0' + h/10), byte('0' + h%10)}
	mm := []byte{byte('0' + m/10), byte('0' + m%10)}
	return string(hh) + ":" + string(mm)
}
