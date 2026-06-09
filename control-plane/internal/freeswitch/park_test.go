package freeswitch

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// flattenPark joins actions into "app=data\n" lines and pulls out the
// valet_park app's data for targeted assertions.
func flattenPark(actions []dialplanAction) (flat, valetData string) {
	var sb strings.Builder
	for _, a := range actions {
		sb.WriteString(a.App + "=" + a.Data + "\n")
		if a.App == "valet_park" {
			valetData = a.Data
		}
	}
	return sb.String(), valetData
}

func TestBuildParkActions(t *testing.T) {
	tid := uuid.New()
	lot := &store.ParkLot{
		ID: uuid.New(), TenantID: tid, Name: "Front desk",
		FeatureCode: "*68", SlotStart: 700, SlotEnd: 779,
	}
	actions := buildParkActions(lot)
	flat, valetData := flattenPark(actions)

	for _, want := range []string{
		"answer=",
		"x_tenant_id=" + tid.String(),
		"x_park_lot_id=" + lot.ID.String(),
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("park actions missing %q\n--- got ---\n%s", want, flat)
		}
	}
	// Auto-assign mode over the configured slot range.
	if !strings.Contains(valetData, "auto in 700 779") {
		t.Errorf("valet_park data = %q, want auto-assign over 700 779", valetData)
	}
	// Lot id namespaced by tenant; the (spaced) name is scrubbed to alnum.
	wantLot := strings.ReplaceAll(tid.String(), "-", "") + "_Frontdesk"
	if !strings.HasPrefix(valetData, wantLot+" ") {
		t.Errorf("valet_park lot id = %q, want prefix %q", valetData, wantLot)
	}
}

func TestBuildRetrieveActions(t *testing.T) {
	tid := uuid.New()
	lot := &store.ParkLot{
		ID: uuid.New(), TenantID: tid, Name: "Front desk",
		FeatureCode: "*68", SlotStart: 700, SlotEnd: 779,
	}
	flat, valetData := flattenPark(buildRetrieveActions(lot, 703))

	if !strings.Contains(flat, "x_park_lot_id="+lot.ID.String()) {
		t.Errorf("retrieve actions missing park lot id:\n%s", flat)
	}
	// Retrieve uses an explicit slot, no "auto in".
	if strings.Contains(valetData, "auto") {
		t.Errorf("retrieve should target an explicit slot, not auto: %q", valetData)
	}
	wantLot := strings.ReplaceAll(tid.String(), "-", "") + "_Frontdesk"
	if valetData != wantLot+" 703" {
		t.Errorf("valet_park retrieve data = %q, want %q", valetData, wantLot+" 703")
	}
}

func TestParseSlot(t *testing.T) {
	cases := []struct {
		in     string
		want   int
		wantOK bool
	}{
		{"703", 703, true},
		{"0", 0, true},
		{"*68", 0, false},
		{"700a", 0, false},
		{"", 0, false},
		{"+15551234567", 0, false},
	}
	for _, c := range cases {
		got, ok := parseSlot(c.in)
		if ok != c.wantOK || (ok && got != c.want) {
			t.Errorf("parseSlot(%q) = (%d, %v), want (%d, %v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}
