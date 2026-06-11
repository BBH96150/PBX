package portal

import (
	"testing"
	"time"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

func TestCDRCSVHeader(t *testing.T) {
	want := []string{
		"started_at", "direction", "from", "to",
		"caller_id_num", "caller_id_name",
		"duration_sec", "billable_sec",
		"disposition", "hangup_cause", "note",
	}
	if len(cdrCSVHeader) != len(want) {
		t.Fatalf("header length = %d, want %d", len(cdrCSVHeader), len(want))
	}
	for i := range want {
		if cdrCSVHeader[i] != want[i] {
			t.Errorf("header[%d] = %q, want %q", i, cdrCSVHeader[i], want[i])
		}
	}
}

func TestCDRToCSVRecord_FullRow(t *testing.T) {
	// Non-UTC start time must be normalized to UTC RFC3339.
	loc := time.FixedZone("EST", -5*3600)
	started := time.Date(2026, 6, 10, 9, 30, 0, 0, loc)
	dur := 42
	bill := 30
	disp := "ANSWERED"

	rec := cdrToCSVRecord(store.CDR{
		Direction:    "inbound",
		FromURI:      "sip:15551234567@example.com",
		ToURI:        "sip:1000@example.com",
		CallerIDNum:  "15551234567",
		CallerIDName: "Acme Corp",
		StartedAt:    started,
		DurationSec:  &dur,
		BillableSec:  &bill,
		Disposition:  &disp,
		HangupCause:  "NORMAL_CLEARING",
		Note:         "follow up",
	})

	if len(rec) != len(cdrCSVHeader) {
		t.Fatalf("record has %d fields, header has %d", len(rec), len(cdrCSVHeader))
	}
	want := []string{
		"2026-06-10T14:30:00Z", "inbound",
		"sip:15551234567@example.com", "sip:1000@example.com",
		"15551234567", "Acme Corp",
		"42", "30",
		"ANSWERED", "NORMAL_CLEARING", "follow up",
	}
	for i := range want {
		if rec[i] != want[i] {
			t.Errorf("field %d (%s) = %q, want %q", i, cdrCSVHeader[i], rec[i], want[i])
		}
	}
}

func TestCDRToCSVRecord_NullAndEmptyFields(t *testing.T) {
	// NULL duration/billable/disposition pointers render as empty strings.
	rec := cdrToCSVRecord(store.CDR{
		Direction:   "outbound",
		StartedAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		DurationSec: nil,
		BillableSec: nil,
		Disposition: nil,
	})

	if rec[0] != "2026-01-02T03:04:05Z" {
		t.Errorf("started_at = %q, want RFC3339", rec[0])
	}
	if rec[6] != "" {
		t.Errorf("duration_sec = %q, want empty for NULL", rec[6])
	}
	if rec[7] != "" {
		t.Errorf("billable_sec = %q, want empty for NULL", rec[7])
	}
	if rec[8] != "" {
		t.Errorf("disposition = %q, want empty for NULL", rec[8])
	}
	if rec[10] != "" {
		t.Errorf("note = %q, want empty", rec[10])
	}
}

func TestCDRToCSVRecord_FormulaInjectionNeutralized(t *testing.T) {
	// Attacker-influenced caller-ID name beginning with '=' must be prefixed so a
	// spreadsheet treats it as literal text, not a formula.
	rec := cdrToCSVRecord(store.CDR{
		Direction:    "inbound",
		StartedAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		CallerIDName: "=HYPERLINK(\"http://evil\")",
	})
	if got := rec[5]; got != "'=HYPERLINK(\"http://evil\")" {
		t.Errorf("caller_id_name = %q, want leading-quote-escaped", got)
	}
}
