package store

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrScheduleNotFound = errors.New("schedule not found for this tenant")

type BusinessSchedule struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	Name      string    `json:"name"`
	Timezone  string    `json:"timezone"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SchedulePeriod is one open window. OpenSec/CloseSec are seconds since
// local midnight (close exclusive).
type SchedulePeriod struct {
	ID       uuid.UUID `json:"id"`
	Weekday  int       `json:"weekday"` // 0 = Sunday
	OpenSec  int       `json:"open_sec"`
	CloseSec int       `json:"close_sec"`
}

type ScheduleHoliday struct {
	ID     uuid.UUID `json:"id"`
	OnDate string    `json:"on_date"` // YYYY-MM-DD
	Name   string    `json:"name,omitempty"`
	IsOpen bool      `json:"is_open"`
}

// scheduleIsOpenAt reports whether the schedule is open at instant t. Holidays
// override the weekly periods (an entry forces the whole day open or closed).
// Pure aside from timezone loading — unit-tested.
func scheduleIsOpenAt(tz string, periods []SchedulePeriod, holidays []ScheduleHoliday, t time.Time) bool {
	loc, err := time.LoadLocation(tz)
	if err != nil || loc == nil {
		loc = time.UTC
	}
	lt := t.In(loc)
	date := lt.Format("2006-01-02")
	for _, h := range holidays {
		if h.OnDate == date {
			return h.IsOpen
		}
	}
	wd := int(lt.Weekday())
	sec := lt.Hour()*3600 + lt.Minute()*60 + lt.Second()
	for _, p := range periods {
		if p.Weekday == wd && sec >= p.OpenSec && sec < p.CloseSec {
			return true
		}
	}
	return false
}

// hhmmssToSec parses "HH:MM:SS" (Postgres time::text) to seconds since midnight.
func hhmmssToSec(s string) int {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) < 2 {
		return 0
	}
	h, _ := strconv.Atoi(parts[0])
	m, _ := strconv.Atoi(parts[1])
	sec := 0
	if len(parts) >= 3 {
		sec, _ = strconv.Atoi(strings.SplitN(parts[2], ".", 2)[0])
	}
	return h*3600 + m*60 + sec
}

// ---- schedule CRUD ----

func (s *Store) CreateSchedule(ctx context.Context, tenantID uuid.UUID, name, tz string) (*BusinessSchedule, error) {
	if tz == "" {
		tz = "UTC"
	}
	const q = `INSERT INTO business_schedules (tenant_id, name, timezone)
	           VALUES ($1,$2,$3)
	           RETURNING id, tenant_id, name, timezone, created_at, updated_at`
	var sc BusinessSchedule
	err := s.DB.QueryRow(ctx, q, tenantID, name, tz).Scan(
		&sc.ID, &sc.TenantID, &sc.Name, &sc.Timezone, &sc.CreatedAt, &sc.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &sc, nil
}

func (s *Store) ListSchedulesForTenant(ctx context.Context, tenantID uuid.UUID) ([]BusinessSchedule, error) {
	const q = `SELECT id, tenant_id, name, timezone, created_at, updated_at
	             FROM business_schedules WHERE tenant_id = $1 ORDER BY name`
	rows, err := s.DB.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BusinessSchedule
	for rows.Next() {
		var sc BusinessSchedule
		if err := rows.Scan(&sc.ID, &sc.TenantID, &sc.Name, &sc.Timezone, &sc.CreatedAt, &sc.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

func (s *Store) GetScheduleForTenant(ctx context.Context, tenantID, id uuid.UUID) (*BusinessSchedule, error) {
	const q = `SELECT id, tenant_id, name, timezone, created_at, updated_at
	             FROM business_schedules WHERE id = $1 AND tenant_id = $2`
	var sc BusinessSchedule
	err := s.DB.QueryRow(ctx, q, id, tenantID).Scan(
		&sc.ID, &sc.TenantID, &sc.Name, &sc.Timezone, &sc.CreatedAt, &sc.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &sc, nil
}

func (s *Store) DeleteScheduleForTenant(ctx context.Context, tenantID, id uuid.UUID) error {
	tag, err := s.DB.Exec(ctx, `DELETE FROM business_schedules WHERE id=$1 AND tenant_id=$2`, id, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrScheduleNotFound
	}
	return nil
}

func (s *Store) ListSchedulePeriods(ctx context.Context, scheduleID uuid.UUID) ([]SchedulePeriod, error) {
	const q = `SELECT id, weekday, open_time::text, close_time::text
	             FROM business_schedule_periods WHERE schedule_id = $1
	            ORDER BY weekday, open_time`
	rows, err := s.DB.Query(ctx, q, scheduleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SchedulePeriod
	for rows.Next() {
		var p SchedulePeriod
		var ot, ct string
		if err := rows.Scan(&p.ID, &p.Weekday, &ot, &ct); err != nil {
			return nil, err
		}
		p.OpenSec, p.CloseSec = hhmmssToSec(ot), hhmmssToSec(ct)
		out = append(out, p)
	}
	return out, rows.Err()
}

// AddSchedulePeriod inserts a weekly open window. open/close are "HH:MM".
func (s *Store) AddSchedulePeriod(ctx context.Context, tenantID, scheduleID uuid.UUID, weekday int, openHM, closeHM string) error {
	const q = `
		INSERT INTO business_schedule_periods (schedule_id, weekday, open_time, close_time)
		SELECT $1, $2, $3::time, $4::time
		 WHERE EXISTS (SELECT 1 FROM business_schedules WHERE id=$1 AND tenant_id=$5)`
	tag, err := s.DB.Exec(ctx, q, scheduleID, weekday, openHM, closeHM, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrScheduleNotFound
	}
	return nil
}

func (s *Store) DeleteSchedulePeriodForTenant(ctx context.Context, tenantID, periodID uuid.UUID) error {
	tag, err := s.DB.Exec(ctx, `
		DELETE FROM business_schedule_periods p USING business_schedules sc
		 WHERE p.id = $1 AND p.schedule_id = sc.id AND sc.tenant_id = $2`, periodID, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrScheduleNotFound
	}
	return nil
}

func (s *Store) ListScheduleHolidays(ctx context.Context, scheduleID uuid.UUID) ([]ScheduleHoliday, error) {
	const q = `SELECT id, on_date::text, COALESCE(name,''), is_open
	             FROM business_schedule_holidays WHERE schedule_id = $1 ORDER BY on_date`
	rows, err := s.DB.Query(ctx, q, scheduleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScheduleHoliday
	for rows.Next() {
		var h ScheduleHoliday
		if err := rows.Scan(&h.ID, &h.OnDate, &h.Name, &h.IsOpen); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Store) AddScheduleHoliday(ctx context.Context, tenantID, scheduleID uuid.UUID, onDate, name string, isOpen bool) error {
	const q = `
		INSERT INTO business_schedule_holidays (schedule_id, on_date, name, is_open)
		SELECT $1, $2::date, NULLIF($3,''), $4
		 WHERE EXISTS (SELECT 1 FROM business_schedules WHERE id=$1 AND tenant_id=$5)
		ON CONFLICT (schedule_id, on_date) DO UPDATE SET name = EXCLUDED.name, is_open = EXCLUDED.is_open`
	_, err := s.DB.Exec(ctx, q, scheduleID, onDate, name, isOpen, tenantID)
	return err
}

func (s *Store) DeleteScheduleHolidayForTenant(ctx context.Context, tenantID, holidayID uuid.UUID) error {
	tag, err := s.DB.Exec(ctx, `
		DELETE FROM business_schedule_holidays h USING business_schedules sc
		 WHERE h.id = $1 AND h.schedule_id = sc.id AND sc.tenant_id = $2`, holidayID, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrScheduleNotFound
	}
	return nil
}

// ---- by-id route resolvers (used to route a closed destination) ----

// ExtensionRouteByID returns the registration target for an active extension.
func (s *Store) ExtensionRouteByID(ctx context.Context, extID uuid.UUID) (*DIDExtensionTarget, error) {
	const q = `
		SELECT e.tenant_id, e.extension, e.sip_username, sd.domain, COALESCE(e.display_name,'')
		  FROM extensions e JOIN sip_domains sd ON sd.id = e.sip_domain_id
		 WHERE e.id = $1 AND e.status = 'active'`
	var t DIDExtensionTarget
	err := s.DB.QueryRow(ctx, q, extID).Scan(
		&t.TenantID, &t.Extension, &t.SIPUsername, &t.SIPDomain, &t.DisplayName)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) RingGroupRouteByID(ctx context.Context, rgID uuid.UUID) (*RingGroupRoutingInfo, error) {
	rg, err := s.ringGroupByID(ctx, rgID)
	if err != nil {
		return nil, err
	}
	members, err := s.fetchRingGroupMembers(ctx, rg.ID)
	if err != nil {
		return nil, err
	}
	return &RingGroupRoutingInfo{Group: *rg, Members: members}, nil
}

func (s *Store) ringGroupByID(ctx context.Context, rgID uuid.UUID) (*RingGroup, error) {
	const q = `
		SELECT id, tenant_id, COALESCE(extension,''), name, strategy, ring_timeout_sec,
		       COALESCE(fallback_kind,''), fallback_id, COALESCE(caller_id_prefix,''),
		       enabled, created_at, updated_at
		  FROM ring_groups WHERE id = $1 AND enabled = true`
	var rg RingGroup
	err := s.DB.QueryRow(ctx, q, rgID).Scan(
		&rg.ID, &rg.TenantID, &rg.Extension, &rg.Name, &rg.Strategy, &rg.RingTimeoutSec,
		&rg.FallbackKind, &rg.FallbackID, &rg.CallerIDPrefix, &rg.Enabled, &rg.CreatedAt, &rg.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &rg, nil
}

func (s *Store) VoicemailRouteByID(ctx context.Context, boxID uuid.UUID) (*DIDVoicemailTarget, error) {
	const q = `
		SELECT vb.id, vb.tenant_id, e.sip_username, sd.domain
		  FROM voicemail_boxes vb
		  JOIN extensions  e  ON e.id  = vb.extension_id
		  JOIN sip_domains sd ON sd.id = e.sip_domain_id
		 WHERE vb.id = $1 AND vb.enabled = true AND e.status = 'active'`
	var t DIDVoicemailTarget
	err := s.DB.QueryRow(ctx, q, boxID).Scan(&t.BoxID, &t.TenantID, &t.SIPUsername, &t.SIPDomain)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) IVRByID(ctx context.Context, ivrID uuid.UUID) (*IVR, error) {
	const q = `
		SELECT id, tenant_id, name, COALESCE(extension,''),
		       greeting_long, greeting_short, invalid_sound, exit_sound,
		       timeout_ms, inter_digit_timeout_ms, max_failures, max_timeouts, digit_len,
		       enabled, created_at, updated_at
		  FROM ivrs WHERE id = $1 AND enabled = true`
	var v IVR
	err := s.DB.QueryRow(ctx, q, ivrID).Scan(
		&v.ID, &v.TenantID, &v.Name, &v.Extension,
		&v.GreetingLong, &v.GreetingShort, &v.InvalidSound, &v.ExitSound,
		&v.TimeoutMS, &v.InterDigitTimeoutMS, &v.MaxFailures, &v.MaxTimeouts, &v.DigitLen,
		&v.Enabled, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (s *Store) QueueByID(ctx context.Context, queueID uuid.UUID) (*Queue, error) {
	const q = `
		SELECT id, tenant_id, COALESCE(extension,''), name, strategy, moh_sound,
		       COALESCE(record_template,''), time_base_score,
		       max_wait_time, max_wait_no_agent, max_wait_no_agent_time_reached,
		       tier_rules_apply, tier_rule_wait_second, tier_rule_no_agent_no_wait,
		       discard_abandoned_after, abandoned_resume_allowed, COALESCE(announce_sound,''),
		       enabled, created_at, updated_at
		  FROM queues WHERE id = $1 AND enabled = true`
	return scanOneQueue(s.DB.QueryRow(ctx, q, queueID))
}

// ScheduledClosedDestination is returned to the dialplan when an inbound DID's
// schedule is currently CLOSED: route to this destination instead of normal.
type ScheduledClosedDestination struct {
	Kind string
	ID   uuid.UUID
}

// ResolveScheduledClosedDestination returns the closed destination for an
// inbound DID when (and only when) the DID has a schedule that is closed at t.
// Returns (nil, nil) for no-schedule, open-now, or a misconfigured closed dest —
// in which case the caller uses normal routing.
func (s *Store) ResolveScheduledClosedDestination(ctx context.Context, e164 string, t time.Time) (*ScheduledClosedDestination, error) {
	const q = `
		SELECT d.schedule_id, COALESCE(d.closed_destination_kind,''), d.closed_destination_id,
		       sc.timezone
		  FROM dids d
		  JOIN business_schedules sc ON sc.id = d.schedule_id
		 WHERE d.e164 = $1 AND d.enabled = true
		 LIMIT 1`
	var schedID uuid.UUID
	var ckind string
	var cid *uuid.UUID
	var tz string
	if err := s.DB.QueryRow(ctx, q, e164).Scan(&schedID, &ckind, &cid, &tz); err != nil {
		return nil, err // includes pgx.ErrNoRows when no schedule
	}
	if ckind == "" || cid == nil {
		return nil, nil // schedule set but no closed dest configured → normal routing
	}
	periods, err := s.ListSchedulePeriods(ctx, schedID)
	if err != nil {
		return nil, err
	}
	holidays, _ := s.ListScheduleHolidays(ctx, schedID)
	if scheduleIsOpenAt(tz, periods, holidays, t) {
		return nil, nil // open → normal routing
	}
	return &ScheduledClosedDestination{Kind: ckind, ID: *cid}, nil
}
