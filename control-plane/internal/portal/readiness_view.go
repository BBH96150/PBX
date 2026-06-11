package portal

import (
	"net/http"

	"github.com/google/uuid"
)

// Tenant readiness / health dashboard: a portal-only, read-only page that
// inspects a tenant's EXISTING configuration and surfaces gaps as a checklist
// of pass / warn / fail items, each with a short explanation and a deep link to
// the page that fixes it. It collects no new data and has no /v1 route — it
// READS existing tables (carrier accounts, DIDs, extensions + e911_location_id,
// schedules, voicemail boxes, tenant MoH/alert-email, users + 2FA) and reuses
// the live ops view's trunk-status helper for carrier presence.

// ReadinessCheck is one row in the readiness checklist.
type ReadinessCheck struct {
	Name    string // short label, e.g. "Phone trunk"
	Status  string // ok | warn | fail
	Detail  string // one-line explanation
	FixHref string // deep link to the page that fixes it (empty when nothing to fix)
}

// readinessSummary is the headline count of a checklist.
type readinessSummary struct {
	OK   int
	Warn int
	Fail int
}

// summarizeReadiness tallies a checklist into ok/warn/fail counts. Pure helper
// so it can be unit-tested independently of the handler; unknown statuses are
// ignored.
func summarizeReadiness(checks []ReadinessCheck) readinessSummary {
	var s readinessSummary
	for _, c := range checks {
		switch c.Status {
		case "ok":
			s.OK++
		case "warn":
			s.Warn++
		case "fail":
			s.Fail++
		}
	}
	return s
}

// overallStatus collapses a summary to a single worst-of status for the page
// banner: fail > warn > ok.
func overallStatus(s readinessSummary) string {
	switch {
	case s.Fail > 0:
		return "fail"
	case s.Warn > 0:
		return "warn"
	default:
		return "ok"
	}
}

func (s *Server) tenantReadiness(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}

	checks := s.buildReadinessChecks(r, tid)
	summary := summarizeReadiness(checks)

	s.renderLayout(w, r, tenant.Name+" · Readiness", "readiness", map[string]any{
		"Tenant":    tenant,
		"NavActive": "readiness",
		"Checks":    checks,
		"Summary":   summary,
		"Overall":   overallStatus(summary),
	})
}

// buildReadinessChecks runs every tenant-scoped readiness check and returns the
// checklist. Each check degrades independently (a query error becomes a warn)
// so the page never 500s on a single bad read.
func (s *Server) buildReadinessChecks(r *http.Request, tid uuid.UUID) []ReadinessCheck {
	ctx := r.Context()
	base := "/admin/tenants/" + tid.String()
	var checks []ReadinessCheck

	// 1. Carrier trunks: any configured? are they up? Reuses the live ops
	// view's trunk-status helper (registration state via the gateway syncer).
	trunks, regN, expN, trunksOK := s.tenantTrunkStatus(ctx, tid)
	switch {
	case !trunksOK:
		// Live status unavailable — fall back to a plain "configured?" check.
		if accts, err := s.store.ListCarrierAccountsForTenant(ctx, tid); err == nil && len(accts) > 0 {
			checks = append(checks, ReadinessCheck{"Carrier trunks", "warn",
				plural(len(accts), "trunk") + " configured; live registration status is unavailable right now.",
				base + "/trunks"})
		} else {
			checks = append(checks, ReadinessCheck{"Carrier trunks", "fail",
				"No carrier trunk configured — you can't make or receive outside calls.",
				base + "/trunks"})
		}
	case len(trunks) == 0:
		checks = append(checks, ReadinessCheck{"Carrier trunks", "fail",
			"No carrier trunk configured — you can't make or receive outside calls.",
			base + "/trunks"})
	case expN > 0 && regN == 0:
		checks = append(checks, ReadinessCheck{"Carrier trunks", "fail",
			"Trunk(s) configured but none are registered (REGED) right now.",
			base + "/trunks"})
	case regN < expN:
		checks = append(checks, ReadinessCheck{"Carrier trunks", "warn",
			regStr(regN, expN), base + "/trunks"})
	default:
		checks = append(checks, ReadinessCheck{"Carrier trunks", "ok",
			plural(len(trunks), "trunk") + " configured and registered.", ""})
	}

	// 2. Inbound numbers (DIDs): at least one? each routed somewhere?
	dids, _ := s.store.ListDIDsForTenant(ctx, tid)
	switch {
	case len(dids) == 0:
		checks = append(checks, ReadinessCheck{"Inbound numbers", "warn",
			"No phone numbers — inbound PSTN calls have nowhere to land.",
			base + "/dids"})
	default:
		noDest := 0
		for _, d := range dids {
			if d.DestinationID == uuid.Nil || d.DestinationKind == "" {
				noDest++
			}
		}
		if noDest > 0 {
			checks = append(checks, ReadinessCheck{"Inbound numbers", "warn",
				plural(noDest, "number") + " of " + plural(len(dids), "number") + " have no destination set.",
				base + "/dids"})
		} else {
			checks = append(checks, ReadinessCheck{"Inbound numbers", "ok",
				plural(len(dids), "number") + " routed.", ""})
		}
	}

	// 3. E911 coverage: every active extension needs a dispatchable location
	// (Kari's Law / RAY BAUM). FAIL when none are covered, WARN when some lack
	// one, OK when all active extensions have a location.
	if cov, err := s.store.E911CoverageForTenant(ctx, tid); err != nil {
		checks = append(checks, ReadinessCheck{"E911 locations", "warn",
			"Couldn't read E911 coverage.", base + "/e911"})
	} else if cov.ActiveExtensions == 0 {
		checks = append(checks, ReadinessCheck{"E911 locations", "warn",
			"No active extensions to assign a dispatchable location yet.", base + "/e911"})
	} else {
		missing := cov.ActiveExtensions - cov.WithLocation
		switch {
		case cov.WithLocation == 0:
			checks = append(checks, ReadinessCheck{"E911 locations", "fail",
				"No extension has a dispatchable E911 location — required for 911 (Kari's Law / RAY BAUM).",
				base + "/e911"})
		case missing > 0:
			checks = append(checks, ReadinessCheck{"E911 locations", "warn",
				plural(missing, "extension") + " of " + plural(cov.ActiveExtensions, "extension") + " lack an E911 location.",
				base + "/e911"})
		default:
			checks = append(checks, ReadinessCheck{"E911 locations", "ok",
				"All " + plural(cov.ActiveExtensions, "extension") + " have a dispatchable location.", ""})
		}
	}

	// 4. Business hours: any schedules defined? (informational.)
	if scheds, err := s.store.ListSchedulesForTenant(ctx, tid); err == nil && len(scheds) > 0 {
		checks = append(checks, ReadinessCheck{"Business hours", "ok",
			plural(len(scheds), "schedule") + " defined for after-hours routing.", ""})
	} else {
		checks = append(checks, ReadinessCheck{"Business hours", "warn",
			"No business-hours schedules — every call routes the same way 24/7.",
			base + "/schedules"})
	}

	// 5. Voicemail: count enabled boxes; warn if some lack a custom greeting.
	if vm, err := s.store.VoicemailCoverageForTenant(ctx, tid); err == nil && vm.Enabled > 0 {
		missing := vm.Enabled - vm.WithGreeting
		if missing > 0 {
			checks = append(checks, ReadinessCheck{"Voicemail", "warn",
				plural(vm.Enabled, "voicemail box") + " enabled; " + plural(missing, "box") + " use the default greeting.",
				""})
		} else {
			checks = append(checks, ReadinessCheck{"Voicemail", "ok",
				plural(vm.Enabled, "voicemail box") + " enabled with custom greetings.", ""})
		}
	} else {
		checks = append(checks, ReadinessCheck{"Voicemail", "warn",
			"No voicemail boxes enabled — missed calls aren't captured.", ""})
	}

	// 6. Music on hold (migration 0025 tenant_moh).
	if moh, err := s.store.GetTenantMoH(ctx, tid); err == nil && moh != "" {
		checks = append(checks, ReadinessCheck{"Music on hold", "ok",
			"Custom hold music configured.", ""})
	} else {
		checks = append(checks, ReadinessCheck{"Music on hold", "warn",
			"No custom hold music — held callers hear the platform default.",
			base})
	}

	// 7. Admin users: at least one tenant_admin?
	if users, err := s.store.ListUsersForTenant(ctx, tid); err == nil {
		admins := 0
		for _, u := range users {
			if u.Role == "tenant_admin" {
				admins++
			}
		}
		if admins == 0 {
			checks = append(checks, ReadinessCheck{"Admin users", "fail",
				"No tenant admin — nobody can manage this workspace's settings.",
				base + "/invites"})
		} else {
			checks = append(checks, ReadinessCheck{"Admin users", "ok",
				plural(admins, "admin user") + " of " + plural(len(users), "member") + ".", ""})
		}
	}

	// 8. Two-factor adoption (informational security posture).
	if n, err := s.store.Count2FAEnrolledUsersForTenant(ctx, tid); err == nil {
		if n == 0 {
			checks = append(checks, ReadinessCheck{"Two-factor auth", "warn",
				"No members have 2FA enrolled — accounts rely on passwords alone.",
				base + "/invites"})
		} else {
			checks = append(checks, ReadinessCheck{"Two-factor auth", "ok",
				plural(n, "member") + " enrolled in 2FA.", ""})
		}
	}

	// 9. Alert destination: tenant alert email (0031) or any webhook (0029)?
	tenantAlert := false
	if t, err := s.store.GetTenant(ctx, tid); err == nil && t.AlertEmail != "" {
		tenantAlert = true
	}
	hooks, _ := s.store.ListWebhookEndpointsForTenant(ctx, tid)
	switch {
	case tenantAlert && len(hooks) > 0:
		checks = append(checks, ReadinessCheck{"Alert destinations", "ok",
			"Alert email and " + plural(len(hooks), "webhook") + " configured.", ""})
	case tenantAlert:
		checks = append(checks, ReadinessCheck{"Alert destinations", "ok",
			"Operational alert email configured.", ""})
	case len(hooks) > 0:
		checks = append(checks, ReadinessCheck{"Alert destinations", "ok",
			plural(len(hooks), "webhook") + " configured for event delivery.", ""})
	default:
		checks = append(checks, ReadinessCheck{"Alert destinations", "warn",
			"No alert email or webhook — operational events go nowhere.",
			base + "/webhooks"})
	}

	return checks
}
