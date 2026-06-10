package portal

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// Tenant wallboard: a full-screen, auto-refreshing "wall display" that combines
// active calls, queue stats, and extension presence on one screen for a NOC /
// office wall monitor. Portal-only — no DB migration, no /v1 route. It reuses
// the existing live-call helpers (scopedActiveCalls), the queue aggregation from
// the queue board, and store.ListExtensionPresenceForTenant for presence.

// wbQueue is the compact per-queue render row for the wallboard.
type wbQueue struct {
	Name    string
	Ext     string
	Waiting int
	Longest string
	Avail   int
	OnCall  int
}

// wallboardData is the fragment render model.
type wallboardData struct {
	TenantID string

	// Active calls.
	HasLive  bool
	FSUp     bool
	Calls    []liveCallView
	CallsN   int

	// Queues (mod_callcenter).
	Queues     []wbQueue
	QueuesUp   bool
	TotalWait  int
	TotalAvail int

	// Presence.
	PresenceUp bool
	OnlineN    int
	TotalExt   int
	Offline    []string // sip usernames / extensions that are offline (compact summary)
	Online     []string
}

// wallboardFragmentData gathers all three data sources, each degrading
// independently so the panel never 500s.
func (s *Server) wallboardFragmentData(ctx context.Context, tid uuid.UUID, set map[string]struct{}) wallboardData {
	d := wallboardData{TenantID: tid.String(), HasLive: s.live != nil}

	// Active calls (reuses the live view's helper + domain scoping).
	calls, fsUp := s.scopedActiveCalls(ctx, set)
	d.Calls = calls
	d.CallsN = len(calls)
	d.FSUp = fsUp

	// Queue stats — same aggregation shape as the queue board, trimmed to
	// headline numbers per queue.
	d.Queues, d.QueuesUp, d.TotalWait, d.TotalAvail = s.wallboardQueues(ctx, tid)

	return d
}

// wallboardQueues returns compact per-queue stats. ok is false when live data is
// unavailable (no ESL client, FS unreachable, or no queues configured).
func (s *Server) wallboardQueues(ctx context.Context, tid uuid.UUID) (out []wbQueue, ok bool, totalWait, totalAvail int) {
	if s.live == nil {
		return nil, false, 0, 0
	}
	queues, _ := listQueues(ctx, s.store, tid)
	if len(queues) == 0 {
		return nil, false, 0, 0
	}

	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	agents, err := s.live.QueueAgents(cctx)
	if err != nil {
		return nil, false, 0, 0
	}
	tiers, _ := s.live.QueueTiers(cctx)

	agentByName := make(map[string]QAgent, len(agents))
	for _, a := range agents {
		agentByName[a.Name] = a
	}

	now := time.Now().Unix()
	for _, q := range queues {
		qName := q.ID.String()
		row := wbQueue{Name: q.Name, Ext: q.Extension}

		members, _ := s.live.QueueMembers(cctx, qName)
		var longest int64
		for _, m := range members {
			if m.State != "Waiting" && m.State != "Trying" {
				continue
			}
			row.Waiting++
			if m.JoinedEpoch > 0 && now > m.JoinedEpoch {
				if secs := now - m.JoinedEpoch; secs > longest {
					longest = secs
				}
			}
		}
		if longest > 0 {
			row.Longest = humandur(int(longest))
		}

		for _, t := range tiers {
			if t.Queue != qName {
				continue
			}
			switch agentBucket(agentByName[t.Agent]) {
			case "avail":
				row.Avail++
			case "oncall":
				row.OnCall++
			}
		}
		totalWait += row.Waiting
		totalAvail += row.Avail
		out = append(out, row)
	}
	return out, true, totalWait, totalAvail
}

// wallboard renders the full-screen standalone wallboard shell (its own HTML
// document, no admin chrome — meant for a wall monitor).
func (s *Server) wallboard(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpls.ExecuteTemplate(w, "wallboard_page", map[string]any{
		"TenantID":   tid.String(),
		"TenantName": tenant.Name,
	}); err != nil {
		slog.Error("wallboard render", "err", err)
	}
}

// wallboardFragment returns just the data panels for htmx polling (~5s).
func (s *Server) wallboardFragment(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	set := s.tenantDomainSet(ctx, tid)
	d := s.wallboardFragmentData(ctx, tid, set)

	// Presence — reuse the just-shipped store method. Degrade to "unavailable"
	// rather than erroring the panel.
	if pres, err := s.store.ListExtensionPresenceForTenant(ctx, tid); err == nil {
		d.PresenceUp = true
		d.TotalExt = len(pres)
		for _, p := range pres {
			label := p.Extension
			if p.DisplayName != "" {
				label = p.Extension + " " + p.DisplayName
			}
			if p.Status == "online" {
				d.OnlineN++
				d.Online = append(d.Online, label)
			} else {
				d.Offline = append(d.Offline, label)
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpls.ExecuteTemplate(w, "wallboard_fragment", d); err != nil {
		slog.Error("wallboard fragment render", "err", err)
	}
}
