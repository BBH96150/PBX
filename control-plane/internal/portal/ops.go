package portal

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// isSuperAdmin reports whether the request's token is platform-scoped (no
// bound tenant) — i.e. allowed to see cross-tenant data.
func (s *Server) isSuperAdmin(ctx context.Context) bool {
	tok := tokenFromCtx(ctx)
	return tok != nil && tok.TenantID == nil
}

type opsTenantRow struct {
	TenantID    uuid.UUID
	Name        string
	Online      int
	TotalExt    int
	ActiveCalls int
}

type opsCallView struct {
	Tenant    string
	Direction string
	CallerNum string
	CalleeNum string
	State     string
	Dur       string
}

func (s *Server) opsLive(w http.ResponseWriter, r *http.Request) {
	if !s.isSuperAdmin(r.Context()) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	s.renderLayout(w, r, "Live ops", "ops_live", map[string]any{})
}

func (s *Server) opsLiveFragment(w http.ResponseWriter, r *http.Request) {
	if !s.isSuperAdmin(r.Context()) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	ctx := r.Context()

	// domain -> tenant attribution map.
	dmap := map[string]store.DomainTenant{}
	dts, _ := s.store.ListSIPDomainTenants(ctx)
	for _, d := range dts {
		dmap[strings.ToLower(d.Domain)] = d
	}

	tenants, _ := s.store.ListTenants(ctx)
	counts, _ := s.store.GetTenantCounts(ctx)

	// Active calls across all tenants.
	var flatCalls []opsCallView
	callsByTenant := map[uuid.UUID]int{}
	fsUp := false
	if s.live != nil {
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		calls, err := s.live.ActiveCalls(cctx)
		cancel()
		if err == nil {
			fsUp = true
			now := time.Now().Unix()
			for _, c := range calls {
				name := "—"
				for _, dom := range c.Domains {
					if d, ok := dmap[strings.ToLower(dom)]; ok {
						name = d.TenantName
						callsByTenant[d.TenantID]++
						break
					}
				}
				dur := ""
				if c.StartEpoch > 0 && now > c.StartEpoch {
					dur = humandur(int(now - c.StartEpoch))
				}
				flatCalls = append(flatCalls, opsCallView{
					Tenant: name, Direction: c.Direction, CallerNum: c.CallerNum,
					CalleeNum: c.CalleeNum, State: c.State, Dur: dur,
				})
			}
		}
	}

	// Presence across all tenants.
	onlineByTenant := map[uuid.UUID]int{}
	presenceUp := false
	if regs, err := s.store.ActiveRegistrations(ctx); err == nil {
		presenceUp = true
		seen := map[string]bool{}
		for _, rg := range regs {
			d, ok := dmap[strings.ToLower(rg.Domain)]
			if !ok {
				continue
			}
			key := d.TenantID.String() + "|" + rg.Username
			if !seen[key] {
				seen[key] = true
				onlineByTenant[d.TenantID]++
			}
		}
	}

	rows := make([]opsTenantRow, 0, len(tenants))
	for _, t := range tenants {
		rows = append(rows, opsTenantRow{
			TenantID: t.ID, Name: t.Name,
			Online: onlineByTenant[t.ID], TotalExt: counts[t.ID].Extensions,
			ActiveCalls: callsByTenant[t.ID],
		})
	}
	// Busiest tenants first, then by name.
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].ActiveCalls != rows[j].ActiveCalls {
			return rows[i].ActiveCalls > rows[j].ActiveCalls
		}
		return rows[i].Name < rows[j].Name
	})

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpls.ExecuteTemplate(w, "ops_live_fragment", map[string]any{
		"Rows":       rows,
		"Calls":      flatCalls,
		"FSUp":       fsUp,
		"HasLive":    s.live != nil,
		"PresenceUp": presenceUp,
	}); err != nil {
		slog.Error("ops live fragment render", "err", err)
	}
}
