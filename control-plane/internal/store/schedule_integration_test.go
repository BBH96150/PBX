//go:build integration

package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestIntegrationScheduleLifecycle exercises schedule + period + holiday CRUD
// against a live Postgres with the full migration set applied.
func TestIntegrationScheduleLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)

	sc, err := s.CreateSchedule(ctx, ten.ID, "Front desk", "America/New_York")
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if sc.Name != "Front desk" || sc.Timezone != "America/New_York" {
		t.Fatalf("unexpected schedule: %+v", sc)
	}

	// Empty timezone defaults to UTC.
	scUTC, err := s.CreateSchedule(ctx, ten.ID, "Default tz", "")
	if err != nil {
		t.Fatalf("CreateSchedule(empty tz): %v", err)
	}
	if scUTC.Timezone != "UTC" {
		t.Errorf("empty tz should default to UTC, got %q", scUTC.Timezone)
	}

	// List shows both, ordered by name.
	list, err := s.ListSchedulesForTenant(ctx, ten.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListSchedulesForTenant: %d schedules, err %v", len(list), err)
	}

	// Get round-trips.
	got, err := s.GetScheduleForTenant(ctx, ten.ID, sc.ID)
	if err != nil || got.ID != sc.ID {
		t.Fatalf("GetScheduleForTenant: %+v err %v", got, err)
	}

	// Add a weekly period (Mon 09:00–17:00) and read it back as seconds.
	if err := s.AddSchedulePeriod(ctx, ten.ID, sc.ID, 1, "09:00", "17:00"); err != nil {
		t.Fatalf("AddSchedulePeriod: %v", err)
	}
	periods, err := s.ListSchedulePeriods(ctx, sc.ID)
	if err != nil || len(periods) != 1 {
		t.Fatalf("ListSchedulePeriods: %d periods, err %v", len(periods), err)
	}
	if periods[0].Weekday != 1 || periods[0].OpenSec != 9*3600 || periods[0].CloseSec != 17*3600 {
		t.Fatalf("unexpected period: %+v", periods[0])
	}

	// The CHECK (close > open) must reject an inverted window.
	if err := s.AddSchedulePeriod(ctx, ten.ID, sc.ID, 2, "17:00", "09:00"); err == nil {
		t.Error("inverted period (close <= open) should be rejected by the CHECK")
	}

	// Add a holiday (closed all day) and an explicit open override.
	if err := s.AddScheduleHoliday(ctx, ten.ID, sc.ID, "2026-12-25", "Christmas", false); err != nil {
		t.Fatalf("AddScheduleHoliday: %v", err)
	}
	if err := s.AddScheduleHoliday(ctx, ten.ID, sc.ID, "2026-12-26", "Boxing Day open", true); err != nil {
		t.Fatalf("AddScheduleHoliday(open): %v", err)
	}
	holidays, err := s.ListScheduleHolidays(ctx, sc.ID)
	if err != nil || len(holidays) != 2 {
		t.Fatalf("ListScheduleHolidays: %d holidays, err %v", len(holidays), err)
	}

	// Upsert on (schedule_id, on_date): re-adding the same date updates in place.
	if err := s.AddScheduleHoliday(ctx, ten.ID, sc.ID, "2026-12-25", "Xmas (renamed)", false); err != nil {
		t.Fatalf("AddScheduleHoliday upsert: %v", err)
	}
	holidays, _ = s.ListScheduleHolidays(ctx, sc.ID)
	if len(holidays) != 2 {
		t.Fatalf("upsert should not add a row; got %d holidays", len(holidays))
	}

	// Delete a period.
	if err := s.DeleteSchedulePeriodForTenant(ctx, ten.ID, periods[0].ID); err != nil {
		t.Fatalf("DeleteSchedulePeriodForTenant: %v", err)
	}
	if p, _ := s.ListSchedulePeriods(ctx, sc.ID); len(p) != 0 {
		t.Errorf("expected 0 periods after delete, got %d", len(p))
	}

	// Delete a holiday.
	if err := s.DeleteScheduleHolidayForTenant(ctx, ten.ID, holidays[0].ID); err != nil {
		t.Fatalf("DeleteScheduleHolidayForTenant: %v", err)
	}

	// Delete the schedule itself; its remaining holiday cascades.
	if err := s.DeleteScheduleForTenant(ctx, ten.ID, sc.ID); err != nil {
		t.Fatalf("DeleteScheduleForTenant: %v", err)
	}
	if _, err := s.GetScheduleForTenant(ctx, ten.ID, sc.ID); err == nil {
		t.Error("schedule should be gone after delete")
	}
}

// TestIntegrationScheduleCrossTenantGuard verifies a tenant cannot read, mutate,
// or delete another tenant's schedule / periods / holidays.
func TestIntegrationScheduleCrossTenantGuard(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tenA := makeTenant(t, s)
	tenB := makeTenant(t, s)

	scA, err := s.CreateSchedule(ctx, tenA.ID, "A hours", "UTC")
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if err := s.AddSchedulePeriod(ctx, tenA.ID, scA.ID, 1, "09:00", "17:00"); err != nil {
		t.Fatalf("AddSchedulePeriod: %v", err)
	}
	if err := s.AddScheduleHoliday(ctx, tenA.ID, scA.ID, "2026-07-04", "Independence Day", false); err != nil {
		t.Fatalf("AddScheduleHoliday: %v", err)
	}
	periods, _ := s.ListSchedulePeriods(ctx, scA.ID)
	holidays, _ := s.ListScheduleHolidays(ctx, scA.ID)

	// Tenant B cannot fetch A's schedule.
	if _, err := s.GetScheduleForTenant(ctx, tenB.ID, scA.ID); err == nil {
		t.Error("tenant B should not be able to Get tenant A's schedule")
	}
	// Tenant B does not see A's schedule in its own list.
	if listB, _ := s.ListSchedulesForTenant(ctx, tenB.ID); len(listB) != 0 {
		t.Errorf("tenant B list should be empty, got %d", len(listB))
	}
	// Tenant B cannot add a period to A's schedule.
	if err := s.AddSchedulePeriod(ctx, tenB.ID, scA.ID, 2, "08:00", "12:00"); !errors.Is(err, ErrScheduleNotFound) {
		t.Errorf("cross-tenant AddSchedulePeriod: want ErrScheduleNotFound, got %v", err)
	}
	// Tenant B cannot add a holiday to A's schedule.
	if err := s.AddScheduleHoliday(ctx, tenB.ID, scA.ID, "2026-01-01", "NYE", false); err != nil {
		// AddScheduleHoliday is a guarded INSERT...WHERE EXISTS; a cross-tenant
		// attempt is a silent no-op (no row inserted), not an error.
		t.Errorf("cross-tenant AddScheduleHoliday should be a no-op, got err %v", err)
	}
	if hs, _ := s.ListScheduleHolidays(ctx, scA.ID); len(hs) != 1 {
		t.Errorf("cross-tenant holiday should not have been added; got %d holidays", len(hs))
	}
	// Tenant B cannot delete A's period.
	if err := s.DeleteSchedulePeriodForTenant(ctx, tenB.ID, periods[0].ID); !errors.Is(err, ErrScheduleNotFound) {
		t.Errorf("cross-tenant DeleteSchedulePeriod: want ErrScheduleNotFound, got %v", err)
	}
	// Tenant B cannot delete A's holiday.
	if err := s.DeleteScheduleHolidayForTenant(ctx, tenB.ID, holidays[0].ID); !errors.Is(err, ErrScheduleNotFound) {
		t.Errorf("cross-tenant DeleteScheduleHoliday: want ErrScheduleNotFound, got %v", err)
	}
	// Tenant B cannot delete A's schedule.
	if err := s.DeleteScheduleForTenant(ctx, tenB.ID, scA.ID); !errors.Is(err, ErrScheduleNotFound) {
		t.Errorf("cross-tenant DeleteSchedule: want ErrScheduleNotFound, got %v", err)
	}
}

// TestIntegrationDIDAttachSchedule attaches/detaches a schedule to a DID and
// verifies the closed-destination resolver used by the inbound dialplan.
func TestIntegrationDIDAttachSchedule(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ten := makeTenant(t, s)
	ext, _ := makeExtension(t, s, ten, "7001")

	carriers, err := s.ListCarriers(ctx)
	if err != nil {
		t.Fatalf("ListCarriers: %v", err)
	}
	if len(carriers) == 0 {
		t.Skip("no seeded carriers (expected CallCentric from migration 0002)")
	}

	e164 := fmt.Sprintf("+1415556%04d", time.Now().UnixNano()%10000)
	did, err := s.CreateDID(ctx, CreateDIDInput{
		TenantID: ten.ID, CarrierID: carriers[0].ID, E164: e164,
		DestinationKind: "extension", DestinationID: ext.ID,
	})
	if err != nil {
		t.Fatalf("CreateDID: %v", err)
	}

	// A schedule open Mon–Fri 09:00–17:00 UTC, with the DID's closed destination
	// pointing back at the extension (any valid kind works for routing).
	sc, err := s.CreateSchedule(ctx, ten.ID, "DID hours", "UTC")
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	for wd := 1; wd <= 5; wd++ {
		if err := s.AddSchedulePeriod(ctx, ten.ID, sc.ID, wd, "09:00", "17:00"); err != nil {
			t.Fatalf("AddSchedulePeriod wd=%d: %v", wd, err)
		}
	}

	// No schedule attached yet → resolver returns ErrNoRows (fall through to normal).
	if cd, err := s.ResolveScheduledClosedDestination(ctx, e164, time.Now()); err == nil && cd != nil {
		t.Errorf("unattached DID should not resolve a closed destination, got %+v", cd)
	}

	// Attach the schedule + closed destination.
	closedID := ext.ID
	if err := s.SetDIDScheduleForTenant(ctx, ten.ID, did.ID, &sc.ID, "extension", &closedID); err != nil {
		t.Fatalf("SetDIDScheduleForTenant: %v", err)
	}

	// Mon 2026-06-01 14:00 UTC → inside open window → nil (normal routing).
	openT := time.Date(2026, 6, 1, 14, 0, 0, 0, time.UTC)
	if cd, err := s.ResolveScheduledClosedDestination(ctx, e164, openT); err != nil || cd != nil {
		t.Errorf("open hour: want (nil,nil), got (%+v, %v)", cd, err)
	}

	// Mon 2026-06-01 03:00 UTC → outside open window → closed destination.
	closedT := time.Date(2026, 6, 1, 3, 0, 0, 0, time.UTC)
	cd, err := s.ResolveScheduledClosedDestination(ctx, e164, closedT)
	if err != nil {
		t.Fatalf("closed hour resolve: %v", err)
	}
	if cd == nil || cd.Kind != "extension" || cd.ID != closedID {
		t.Fatalf("closed hour: want extension/%s, got %+v", closedID, cd)
	}

	// A holiday forces closed even during open hours.
	if err := s.AddScheduleHoliday(ctx, ten.ID, sc.ID, "2026-06-01", "Test holiday", false); err != nil {
		t.Fatalf("AddScheduleHoliday: %v", err)
	}
	if cd, err := s.ResolveScheduledClosedDestination(ctx, e164, openT); err != nil || cd == nil {
		t.Errorf("holiday during open hours should resolve closed dest, got (%+v, %v)", cd, err)
	}

	// Detach the schedule → resolver no longer fires.
	if err := s.SetDIDScheduleForTenant(ctx, ten.ID, did.ID, nil, "", nil); err != nil {
		t.Fatalf("SetDIDScheduleForTenant(detach): %v", err)
	}
	if cd, err := s.ResolveScheduledClosedDestination(ctx, e164, closedT); err == nil && cd != nil {
		t.Errorf("detached DID should not resolve a closed destination, got %+v", cd)
	}

	// Cross-tenant guard: another tenant cannot attach a schedule to this DID.
	tenB := makeTenant(t, s)
	if err := s.SetDIDScheduleForTenant(ctx, tenB.ID, did.ID, &sc.ID, "extension", &closedID); !errors.Is(err, ErrDIDNotFound) {
		t.Errorf("cross-tenant SetDIDSchedule: want ErrDIDNotFound, got %v", err)
	}
}
