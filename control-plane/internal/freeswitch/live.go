package freeswitch

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// ActiveCall is one in-progress call on FreeSWITCH, collapsed from its
// individual channel legs (a bridged call has two legs sharing a call_uuid).
type ActiveCall struct {
	CallUUID   string   // FS call_uuid (groups the legs)
	KillUUID   string   // a channel uuid to pass to uuid_kill to tear the call down
	Direction  string   // inbound | outbound | (other)
	CallerNum  string   // calling party number
	CallerName string   // calling party name (CID)
	CalleeNum  string   // called destination
	State      string   // FS callstate: ACTIVE / RINGING / EARLY / DOWN …
	StartEpoch int64    // earliest leg creation (unix seconds) — for duration
	Domains    []string // SIP domains seen on any leg — used for tenant scoping
}

// HasDomain reports whether any leg of the call belongs to one of the given
// (lowercased) SIP domains. Used to scope the live view to a tenant.
func (a ActiveCall) HasDomain(domains map[string]struct{}) bool {
	for _, d := range a.Domains {
		if _, ok := domains[strings.ToLower(d)]; ok {
			return true
		}
	}
	return false
}

// ActiveCalls returns the calls currently up on FreeSWITCH, grouped from
// `show channels as json`. Returns ErrNotConnected (via CallAPISync) when ESL
// is down so callers can degrade gracefully rather than error the page.
func (c *ESLClient) ActiveCalls(ctx context.Context) ([]ActiveCall, error) {
	body, err := c.CallAPISync(ctx, "show channels as json")
	if err != nil {
		return nil, err
	}
	return parseActiveCalls(body), nil
}

// Hangup tears down a channel (and, for a bridged call, its peer) via uuid_kill.
func (c *ESLClient) Hangup(ctx context.Context, uuid string) error {
	_, err := c.CallAPISync(ctx, "uuid_kill "+uuid)
	return err
}

// parseActiveCalls is split out for unit testing against captured FS output.
func parseActiveCalls(body string) []ActiveCall {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	var wrap struct {
		RowCount int              `json:"row_count"`
		Rows     []map[string]any `json:"rows"`
	}
	if err := json.Unmarshal([]byte(body), &wrap); err != nil {
		return nil
	}
	byCall := map[string]*ActiveCall{}
	order := []string{}
	for _, row := range wrap.Rows {
		uuid := str(row, "uuid")
		callUUID := str(row, "call_uuid")
		if callUUID == "" {
			callUUID = uuid
		}
		if callUUID == "" {
			continue
		}
		ac, ok := byCall[callUUID]
		if !ok {
			ac = &ActiveCall{CallUUID: callUUID}
			byCall[callUUID] = ac
			order = append(order, callUUID)
		}
		// Track the earliest leg as the A-leg / kill target.
		epoch := atoiSafe(str(row, "created_epoch"))
		isFirstOrEarlier := ac.StartEpoch == 0 || (epoch > 0 && epoch < ac.StartEpoch)
		dir := str(row, "direction")
		// Prefer the inbound leg for caller/callee labelling; otherwise take
		// whatever we see first.
		if ac.CallerNum == "" || dir == "inbound" {
			ac.Direction = dir
			ac.CallerNum = firstNonEmpty(str(row, "cid_num"), ac.CallerNum)
			ac.CallerName = firstNonEmpty(str(row, "cid_name"), ac.CallerName)
			ac.CalleeNum = firstNonEmpty(str(row, "callee_num"), str(row, "dest"), ac.CalleeNum)
			ac.State = firstNonEmpty(str(row, "callstate"), ac.State)
		}
		if isFirstOrEarlier {
			ac.StartEpoch = epoch
			if uuid != "" {
				ac.KillUUID = uuid
			}
		}
		if ac.KillUUID == "" && uuid != "" {
			ac.KillUUID = uuid
		}
		if d := domainOf(row); d != "" {
			ac.Domains = appendUnique(ac.Domains, d)
		}
	}
	if len(order) == 0 {
		return nil
	}
	out := make([]ActiveCall, 0, len(order))
	for _, k := range order {
		out = append(out, *byCall[k])
	}
	// Most recent first.
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartEpoch > out[j].StartEpoch })
	return out
}

// domainOf extracts the SIP domain from a channel row, preferring presence_id
// (user@domain) and falling back to common FS variables.
func domainOf(row map[string]any) string {
	for _, key := range []string{"presence_id", "sip_from_host", "sip_to_host"} {
		v := str(row, key)
		if at := strings.LastIndex(v, "@"); at >= 0 {
			v = v[at+1:]
		}
		if v != "" {
			return v
		}
	}
	return ""
}

func str(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	default:
		return ""
	}
}

func atoiSafe(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}
