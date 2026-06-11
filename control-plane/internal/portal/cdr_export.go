package portal

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"time"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// cdrCSVHeader is the column order for the CDR export, matching cdrToCSVRecord.
var cdrCSVHeader = []string{
	"started_at", "direction", "from", "to",
	"caller_id_num", "caller_id_name",
	"duration_sec", "billable_sec",
	"disposition", "hangup_cause", "note",
}

// cdrToCSVRecord renders one CDR as a CSV row in cdrCSVHeader order. It is a pure
// helper (no I/O) so it can be unit-tested: timestamps are RFC3339 in UTC,
// durations are integer seconds ("" when NULL), and attacker-influenced text
// fields are run through csvSafe to neutralize spreadsheet formula injection.
func cdrToCSVRecord(c store.CDR) []string {
	intStr := func(p *int) string {
		if p == nil {
			return ""
		}
		return strconv.Itoa(*p)
	}
	disp := ""
	if c.Disposition != nil {
		disp = *c.Disposition
	}
	return []string{
		c.StartedAt.UTC().Format(time.RFC3339), c.Direction,
		csvSafe(c.FromURI), csvSafe(c.ToURI),
		csvSafe(c.CallerIDNum), csvSafe(c.CallerIDName),
		intStr(c.DurationSec), intStr(c.BillableSec),
		disp, csvSafe(c.HangupCause), csvSafe(c.Note),
	}
}

// tenantCDRsCSV streams the filtered call log as a CSV download. It applies the
// exact same tenant-access guard (parseTenantParam) and query filters
// (cdrFilterFromQuery) as the on-screen CDR list, with the export limit capped at
// 10k rows so the stream stays bounded.
func (s *Server) tenantCDRsCSV(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if _, err := s.store.GetTenant(r.Context(), tid); err != nil {
		s.errPage(w, r, err)
		return
	}
	filter := cdrFilterFromQuery(r.URL.Query(), 10000)
	cdrs, err := s.store.ListCDRsFilteredForTenant(r.Context(), tid, filter)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		`attachment; filename="cdrs-`+time.Now().UTC().Format("2006-01-02")+`.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write(cdrCSVHeader)
	for _, c := range cdrs {
		_ = cw.Write(cdrToCSVRecord(c))
	}
	cw.Flush()
	_ = cw.Error()
}
