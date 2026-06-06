package portal

import (
	"net/http"
	"strconv"
)

// checkRow is one readiness check on the setup page.
type checkRow struct {
	Label    string
	Status   string // ok | warn | fail
	Detail   string
	FixURL   string
	FixLabel string
}

// tenantSetupCheck runs live readiness checks for a tenant and renders a
// pass/warn/fail report — a "test my setup" diagnostic for onboarding.
func (s *Server) tenantSetupCheck(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	ctx := r.Context()
	base := "/admin/tenants/" + tid.String()

	domains := mustSIPDomains(ctx, s.store, tid)
	hasPrimary := false
	for _, d := range domains {
		if d.IsPrimary {
			hasPrimary = true
			break
		}
	}
	exts := mustExtensions(ctx, s.store, tid)
	dids, _ := s.store.ListDIDsForTenant(ctx, tid)
	trunks, regN, expN, trunksOK := s.tenantTrunkStatus(ctx, tid)
	online, presenceUp := s.tenantPresence(ctx, s.tenantDomainSet(ctx, tid))

	var rows []checkRow

	// SIP domain
	if hasPrimary {
		rows = append(rows, checkRow{"SIP domain", "ok", "Primary domain configured.", "", ""})
	} else {
		rows = append(rows, checkRow{"SIP domain", "fail", "No primary SIP domain — extensions can't register.", base, "Open overview"})
	}

	// Extensions
	if len(exts) > 0 {
		rows = append(rows, checkRow{"Extensions", "ok", plural(len(exts), "active extension"), "", ""})
	} else {
		rows = append(rows, checkRow{"Extensions", "fail", "No extensions yet.", base, "Add extensions"})
	}

	// Trunk configured + registered
	switch {
	case !trunksOK || len(trunks) == 0:
		rows = append(rows, checkRow{"Phone trunk", "fail", "No SIP trunk configured — you can't make or receive outside calls.", base + "/trunks", "Set up a trunk"})
	case expN > 0 && regN == 0:
		rows = append(rows, checkRow{"Phone trunk", "fail", "Trunk(s) configured but none are registered (REGED).", base + "/trunks", "Check trunks"})
	case regN < expN:
		rows = append(rows, checkRow{"Phone trunk", "warn", regStr(regN, expN), base + "/trunks", "Check trunks"})
	default:
		rows = append(rows, checkRow{"Phone trunk", "ok", regStr(regN, expN), "", ""})
	}

	// Phone numbers
	if len(dids) > 0 {
		rows = append(rows, checkRow{"Phone numbers", "ok", plural(len(dids), "number") + " routed.", "", ""})
	} else {
		rows = append(rows, checkRow{"Phone numbers", "warn", "No phone numbers routed — inbound PSTN calls have nowhere to land.", base + "/dids", "Add a number"})
	}

	// Presence (phones registered)
	switch {
	case !presenceUp:
		rows = append(rows, checkRow{"Phones online", "warn", "Live presence unavailable.", base + "/live", "Open live view"})
	case len(online) == 0:
		rows = append(rows, checkRow{"Phones online", "warn", "No extensions are registered right now — register a softphone/desk phone.", base + "/live", "Open live view"})
	default:
		rows = append(rows, checkRow{"Phones online", "ok", plural(len(online), "extension") + " registered now.", "", ""})
	}

	fails, warns := 0, 0
	for _, c := range rows {
		switch c.Status {
		case "fail":
			fails++
		case "warn":
			warns++
		}
	}
	overall := "ok"
	if warns > 0 {
		overall = "warn"
	}
	if fails > 0 {
		overall = "fail"
	}

	s.renderLayout(w, r, tenant.Name+" · Setup check", "setup", map[string]any{
		"Tenant":  tenant,
		"Rows":    rows,
		"Overall": overall,
		"Fails":   fails,
		"Warns":   warns,
	})
}

func plural(n int, noun string) string {
	s := strconv.Itoa(n) + " " + noun
	if n != 1 {
		s += "s"
	}
	return s
}

func regStr(reg, exp int) string {
	return strconv.Itoa(reg) + "/" + strconv.Itoa(exp) + " trunk(s) registered."
}
