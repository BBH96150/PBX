package freeswitch

import (
	"context"
	"strings"
)

// mod_callcenter "... list" commands emit a leading pipe-delimited header row
// of column names, then data rows, then a trailing "+OK". We parse by header
// name (not fixed position) so column-order changes across FS versions don't
// break us.

// CCAgent is one call-center agent's live state.
type CCAgent struct {
	Name   string // e.g. "agent_<extID>"
	Status string // Available | On Break | Logged Out
	State  string // Waiting | Receiving | In a queue call
}

// CCTier links an agent to a queue.
type CCTier struct {
	Queue string // queue name (our queue UUID)
	Agent string
}

// CCMember is a caller in a queue.
type CCMember struct {
	CIDNum      string
	CIDName     string
	JoinedEpoch int64
	State       string // Waiting | Trying | Answered | Abandoned
}

// parseCCList parses mod_callcenter list output into rows keyed by header name.
func parseCCList(body string) []map[string]string {
	var header []string
	var rows []map[string]string
	for _, raw := range strings.Split(body, "\n") {
		ln := strings.TrimRight(raw, "\r")
		t := strings.TrimSpace(ln)
		if t == "" || t == "+OK" || strings.HasPrefix(t, "-ERR") {
			continue
		}
		if !strings.Contains(ln, "|") {
			continue
		}
		cols := strings.Split(ln, "|")
		if header == nil {
			header = cols
			continue
		}
		m := make(map[string]string, len(header))
		for i, c := range cols {
			if i < len(header) {
				m[header[i]] = c
			}
		}
		rows = append(rows, m)
	}
	return rows
}

func (c *ESLClient) CCAgents(ctx context.Context) ([]CCAgent, error) {
	body, err := c.CallAPISync(ctx, "callcenter_config agent list")
	if err != nil {
		return nil, err
	}
	var out []CCAgent
	for _, m := range parseCCList(body) {
		out = append(out, CCAgent{Name: m["name"], Status: m["status"], State: m["state"]})
	}
	return out, nil
}

func (c *ESLClient) CCTiers(ctx context.Context) ([]CCTier, error) {
	body, err := c.CallAPISync(ctx, "callcenter_config tier list")
	if err != nil {
		return nil, err
	}
	var out []CCTier
	for _, m := range parseCCList(body) {
		out = append(out, CCTier{Queue: m["queue"], Agent: m["agent"]})
	}
	return out, nil
}

func (c *ESLClient) CCMembers(ctx context.Context, queueName string) ([]CCMember, error) {
	body, err := c.CallAPISync(ctx, "callcenter_config queue list members "+queueName)
	if err != nil {
		return nil, err
	}
	var out []CCMember
	for _, m := range parseCCList(body) {
		joined := m["joined_epoch"]
		if joined == "" {
			joined = m["system_epoch"]
		}
		out = append(out, CCMember{
			CIDNum:      m["cid_number"],
			CIDName:     m["cid_name"],
			JoinedEpoch: atoiSafe(joined),
			State:       m["state"],
		})
	}
	return out, nil
}
