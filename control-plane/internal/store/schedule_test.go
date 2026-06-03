package store

import (
	"testing"
	"time"
)

func TestScheduleIsOpenAt(t *testing.T) {
	// Mon–Fri 09:00–17:00 in New York.
	periods := []SchedulePeriod{}
	for wd := 1; wd <= 5; wd++ {
		periods = append(periods, SchedulePeriod{Weekday: wd, OpenSec: 9 * 3600, CloseSec: 17 * 3600})
	}
	ny := "America/New_York"
	at := func(s string) time.Time {
		tm, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("bad time %q: %v", s, err)
		}
		return tm
	}

	cases := []struct {
		name     string
		tz       string
		holidays []ScheduleHoliday
		when     time.Time
		want     bool
	}{
		// 2026-06-01 is a Monday. 14:00 EDT = 18:00Z.
		{"mon midday open", ny, nil, at("2026-06-01T18:00:00Z"), true},
		// 08:30 EDT = 12:30Z — before open.
		{"mon before open", ny, nil, at("2026-06-01T12:30:00Z"), false},
		// 17:00 EDT = 21:00Z — close is exclusive.
		{"mon at close", ny, nil, at("2026-06-01T21:00:00Z"), false},
		// 16:59 EDT = 20:59Z — still open.
		{"mon just before close", ny, nil, at("2026-06-01T20:59:00Z"), true},
		// 2026-06-06 is a Saturday.
		{"saturday closed", ny, nil, at("2026-06-06T18:00:00Z"), false},
		// Timezone matters: 14:00Z on Monday is 10:00 EDT (open) but 09:00 in
		// a UTC schedule would also be open; pick a time that differs. 02:00 EDT
		// Monday = 06:00Z — closed in NY...
		{"tz respected (NY night)", ny, nil, at("2026-06-01T06:00:00Z"), false},
		// ...but a UTC schedule at 06:00Z Monday is also before 09:00 → closed.
		{"utc before open", "UTC", nil, at("2026-06-01T06:00:00Z"), false},
		{"utc midday open", "UTC", nil, at("2026-06-01T12:00:00Z"), true},
		// Holiday forces closed on an otherwise-open Monday.
		{"holiday closes open day", ny, []ScheduleHoliday{{OnDate: "2026-06-01", IsOpen: false}}, at("2026-06-01T18:00:00Z"), false},
		// Holiday forces open on an otherwise-closed Saturday.
		{"holiday opens closed day", ny, []ScheduleHoliday{{OnDate: "2026-06-06", IsOpen: true}}, at("2026-06-06T18:00:00Z"), true},
		// Bad timezone falls back to UTC.
		{"bad tz -> utc", "Not/AZone", nil, at("2026-06-01T12:00:00Z"), true},
	}
	for _, c := range cases {
		if got := scheduleIsOpenAt(c.tz, periods, c.holidays, c.when); got != c.want {
			t.Errorf("%s: scheduleIsOpenAt = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestHHMMSSToSec(t *testing.T) {
	cases := map[string]int{
		"09:00:00":   9 * 3600,
		"17:30":      17*3600 + 30*60,
		"00:00:00":   0,
		"23:59:59":   23*3600 + 59*60 + 59,
		"13:05:00.5": 13*3600 + 5*60,
	}
	for in, want := range cases {
		if got := hhmmssToSec(in); got != want {
			t.Errorf("hhmmssToSec(%q) = %d, want %d", in, got, want)
		}
	}
}
