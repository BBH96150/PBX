package portal

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// pagingList renders the Paging / PTT page: all of a tenant's paging groups,
// and — when ?group=<id> is set — the selected group's members plus an
// add-member form. Mirrors the contacts page shape.
func (s *Server) pagingList(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	groups, _ := s.store.ListPagingGroupsForTenant(r.Context(), tid)

	data := map[string]any{
		"Tenant":    tenant,
		"NavActive": "paging",
		"Groups":    groups,
	}

	// Optional drill-in: show one group's members + the extensions that can
	// still be added to it.
	if gid := strings.TrimSpace(r.URL.Query().Get("group")); gid != "" {
		if id, err := uuid.Parse(gid); err == nil {
			if g, err := s.store.GetPagingGroupForTenant(r.Context(), tid, id); err == nil {
				members, _ := s.store.ListPagingMembersDetailed(r.Context(), g.ID)
				inGroup := make(map[uuid.UUID]bool, len(members))
				for _, m := range members {
					inGroup[m.ExtensionID] = true
				}
				exts, _ := s.store.ListExtensionsForTenant(r.Context(), tid)
				var addable []store.Extension
				for _, e := range exts {
					if !inGroup[e.ID] {
						addable = append(addable, e)
					}
				}
				data["Selected"] = g
				data["Members"] = members
				data["Addable"] = addable
			}
		}
	}

	s.renderLayout(w, r, tenant.Name+" · Paging", "paging", data)
}

func (s *Server) pagingCreate(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/paging"
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Redirect(w, r, redirect+"?err=Name+is+required", http.StatusSeeOther)
		return
	}
	mode := strings.TrimSpace(r.FormValue("mode"))
	mcastPort := 0
	if p := strings.TrimSpace(r.FormValue("multicast_port")); p != "" {
		mcastPort, _ = strconv.Atoi(p)
	}
	g, err := s.store.CreatePagingGroup(r.Context(), store.CreatePagingGroupInput{
		TenantID:      tid,
		Extension:     strings.TrimSpace(r.FormValue("extension")),
		Name:          name,
		Mode:          mode,
		MulticastAddr: strings.TrimSpace(r.FormValue("multicast_addr")),
		MulticastPort: mcastPort,
	})
	if err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "paging_group.created", "paging_group", &g.ID, map[string]any{"name": name, "mode": g.Mode})
	http.Redirect(w, r, redirect+"?group="+g.ID.String()+"&flash=Paging+group+created.", http.StatusSeeOther)
}

func (s *Server) pagingDelete(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/paging"
	if err := s.store.DeletePagingGroupForTenant(r.Context(), tid, id); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "paging_group.deleted", "paging_group", &id, nil)
	http.Redirect(w, r, redirect+"?flash=Paging+group+deleted.", http.StatusSeeOther)
}

func (s *Server) pagingToggle(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/paging"
	enabled := r.FormValue("enabled") == "true"
	if err := s.store.SetPagingGroupEnabled(r.Context(), tid, id, enabled); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "paging_group.toggled", "paging_group", &id, map[string]any{"enabled": enabled})
	http.Redirect(w, r, redirect+"?group="+id.String()+"&flash=Updated.", http.StatusSeeOther)
}

func (s *Server) pagingMemberAdd(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	gid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/paging?group=" + gid.String()
	// Verify the group belongs to this tenant before touching members.
	if _, err := s.store.GetPagingGroupForTenant(r.Context(), tid, gid); err != nil {
		s.flashErr(w, r, "/admin/tenants/"+tid.String()+"/paging", err)
		return
	}
	extID, err := uuid.Parse(strings.TrimSpace(r.FormValue("extension_id")))
	if err != nil {
		http.Redirect(w, r, redirect+"&err=Pick+an+extension", http.StatusSeeOther)
		return
	}
	if _, err := s.store.AddPagingMember(r.Context(), gid, extID); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "paging_group.member_added", "paging_group", &gid, map[string]any{"extension_id": extID.String()})
	http.Redirect(w, r, redirect+"&flash=Member+added.", http.StatusSeeOther)
}

func (s *Server) pagingMemberRemove(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	gid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	mid, err := uuid.Parse(chi.URLParam(r, "memberID"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	redirect := "/admin/tenants/" + tid.String() + "/paging?group=" + gid.String()
	if err := s.store.RemovePagingMemberForTenant(r.Context(), tid, mid); err != nil {
		s.flashErr(w, r, redirect, err)
		return
	}
	s.auditNested(r, tid, "paging_group.member_removed", "paging_group", &gid, map[string]any{"member_id": mid.String()})
	http.Redirect(w, r, redirect+"&flash=Member+removed.", http.StatusSeeOther)
}
