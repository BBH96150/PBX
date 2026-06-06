package portal

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
)

// qbAgent / qbCaller / qbQueue are the render models for the live queue board.
type qbAgent struct {
	Label  string // "Ext 101" or the raw agent name
	Status string // raw FS status
	Bucket string // avail | oncall | out
}

type qbCaller struct {
	Num  string
	Name string
	Wait string // humandur since joined
}

type qbQueue struct {
	Name    string
	Ext     string
	Waiting int
	Longest string
	Callers []qbCaller
	Avail   int
	OnCall  int
	Out     int
	Agents  []qbAgent
}

func agentBucket(a QAgent) string {
	switch {
	case a.Status == "Logged Out" || a.Status == "On Break":
		return "out"
	case strings.Contains(a.State, "call") || a.State == "Receiving":
		return "oncall"
	default:
		return "avail" // Available + Waiting
	}
}

func (s *Server) queueBoard(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tid)
	if err != nil {
		s.errPage(w, r, err)
		return
	}
	s.renderLayout(w, r, tenant.Name+" · Queue board", "queue_board", map[string]any{
		"Tenant": tenant,
	})
}

func (s *Server) queueBoardFragment(w http.ResponseWriter, r *http.Request) {
	tid, ok := s.parseTenantParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	queues, _ := listQueues(ctx, s.store, tid)

	data := map[string]any{
		"HasLive":  s.live != nil,
		"FSUp":     false,
		"Queues":   []qbQueue{},
		"NoQueues": len(queues) == 0,
	}
	if s.live == nil || len(queues) == 0 {
		s.renderQueueFragment(w, data)
		return
	}

	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	agents, err := s.live.QueueAgents(cctx)
	if err != nil {
		s.renderQueueFragment(w, data) // FSUp stays false → "unavailable" banner
		return
	}
	data["FSUp"] = true
	tiers, _ := s.live.QueueTiers(cctx)

	// agentName -> state, and ext-number label lookup.
	agentByName := make(map[string]QAgent, len(agents))
	for _, a := range agents {
		agentByName[a.Name] = a
	}
	extByID := map[string]string{}
	for _, e := range mustExtensions(ctx, s.store, tid) {
		extByID[e.ID.String()] = e.Extension
	}
	label := func(agentName string) string {
		if id := strings.TrimPrefix(agentName, "agent_"); id != agentName {
			if num, ok := extByID[id]; ok {
				return "Ext " + num
			}
		}
		return agentName
	}

	now := time.Now().Unix()
	var boards []qbQueue
	for _, q := range queues {
		qName := q.ID.String()
		board := qbQueue{Name: q.Name, Ext: q.Extension}

		members, _ := s.live.QueueMembers(cctx, qName)
		var longest int64
		for _, m := range members {
			if m.State != "Waiting" && m.State != "Trying" {
				continue
			}
			board.Waiting++
			wait := ""
			if m.JoinedEpoch > 0 && now > m.JoinedEpoch {
				secs := now - m.JoinedEpoch
				if secs > longest {
					longest = secs
				}
				wait = humandur(int(secs))
			}
			nm := m.CIDName
			board.Callers = append(board.Callers, qbCaller{Num: m.CIDNum, Name: nm, Wait: wait})
		}
		if longest > 0 {
			board.Longest = humandur(int(longest))
		}

		for _, t := range tiers {
			if t.Queue != qName {
				continue
			}
			a := agentByName[t.Agent]
			b := agentBucket(a)
			switch b {
			case "avail":
				board.Avail++
			case "oncall":
				board.OnCall++
			default:
				board.Out++
			}
			board.Agents = append(board.Agents, qbAgent{Label: label(t.Agent), Status: a.Status, Bucket: b})
		}
		sort.SliceStable(board.Agents, func(i, j int) bool { return board.Agents[i].Label < board.Agents[j].Label })
		boards = append(boards, board)
	}
	data["Queues"] = boards
	s.renderQueueFragment(w, data)
}

func (s *Server) renderQueueFragment(w http.ResponseWriter, data map[string]any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpls.ExecuteTemplate(w, "queue_board_fragment", data); err != nil {
		slog.Error("queue board fragment render", "err", err)
	}
}
