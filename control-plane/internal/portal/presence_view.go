package portal

import "net/http"

// presenceList renders the Presence page: each active extension's live
// registration status (online/offline), derived from Kamailio's usrloc
// location table via the store. Read-only — mirrors conference_view.go's list
// shape (no create/delete).
//
// OUT OF SCOPE here (each needs box validation / config + restart, not a
// CI-verifiable code change):
//   - Live call overlay (ringing / on-call) — would come from ESL active calls.
//   - Desk-phone BLF (Busy Lamp Field): Kamailio presence modules (presence,
//     presence_dialoginfo, pua_dialoginfo) + SUBSCRIBE/NOTIFY dialog-info
//     routing in kamailio.cfg.
//   - ZTP BLF / speed-dial keys in the per-vendor device provisioning templates.
func (s *Server) presenceList(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	// Degrade gracefully: if the location table is unavailable, show the page
	// with whatever the store returned (empty) rather than erroring out.
	presence, _ := s.store.ListExtensionPresenceForTenant(r.Context(), tid)

	data := map[string]any{
		"Tenant":    tenant,
		"NavActive": "presence",
		"Presence":  presence,
	}
	s.renderLayout(w, r, tenant.Name+" · Presence", "presence", data)
}
