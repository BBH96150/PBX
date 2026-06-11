package portal

import (
	"encoding/csv"
	"net/http"
	"strings"
	"time"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// auditCSVMaxRows caps the export so the stream stays bounded.
const auditCSVMaxRows = 10000

// auditCSVHeader is the column order for the audit export, matching auditToCSVRecord.
var auditCSVHeader = []string{
	"created_at", "actor", "event",
	"target", "ip_address", "detail",
}

// auditToCSVRecord renders one audit entry as a CSV row in auditCSVHeader order.
// It is a pure helper (no I/O) so it can be unit-tested: the timestamp is
// RFC3339 in UTC, the actor falls back from email to user id to token id, the
// target combines target_type and target_id, the detail is the raw JSON
// payload, and every attacker-influenced text field is run through csvSafe to
// neutralize spreadsheet formula injection.
func auditToCSVRecord(e store.AuditEntry) []string {
	actor := e.ActorEmail
	if actor == "" && e.ActorUserID != nil {
		actor = e.ActorUserID.String()
	}
	if actor == "" && e.ActorTokenID != nil {
		actor = "token:" + e.ActorTokenID.String()
	}
	target := e.TargetType
	if e.TargetID != nil {
		if target != "" {
			target += ":"
		}
		target += e.TargetID.String()
	}
	return []string{
		e.CreatedAt.UTC().Format(time.RFC3339),
		csvSafe(actor), csvSafe(e.Event),
		csvSafe(target), csvSafe(e.IPAddress), csvSafe(string(e.Payload)),
	}
}

// tenantAuditCSV streams the filtered audit log as a CSV download. It applies
// the exact same tenant-access guard (parseTenantParam) and query filters
// (event + actor substrings) as the on-screen audit list, with the export limit
// capped at auditCSVMaxRows so the stream stays bounded.
func (s *Server) tenantAuditCSV(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if _, err := s.store.GetTenant(r.Context(), tid); err != nil {
		s.errPage(w, r, err)
		return
	}
	eventFilter := strings.TrimSpace(r.URL.Query().Get("event"))
	actorFilter := strings.TrimSpace(r.URL.Query().Get("actor"))
	entries, err := s.store.ListAuditForTenantFiltered(r.Context(), tid, eventFilter, actorFilter, auditCSVMaxRows)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		`attachment; filename="audit-`+time.Now().UTC().Format("2006-01-02")+`.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write(auditCSVHeader)
	for _, e := range entries {
		_ = cw.Write(auditToCSVRecord(e))
	}
	cw.Flush()
	_ = cw.Error()
}
