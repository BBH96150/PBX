package portal

import "testing"

func TestAgentBucket(t *testing.T) {
	cases := []struct {
		status, state, want string
	}{
		{"Logged Out", "Waiting", "out"},
		{"On Break", "Waiting", "out"},
		{"Available", "Waiting", "avail"},
		{"Available", "In a queue call", "oncall"},
		{"Available", "Receiving", "oncall"},
		{"", "", "avail"}, // unknown status defaults to avail bucket
	}
	for _, c := range cases {
		if got := agentBucket(QAgent{Status: c.status, State: c.state}); got != c.want {
			t.Errorf("agentBucket(status=%q,state=%q)=%q want %q", c.status, c.state, got, c.want)
		}
	}
}

func TestLiveCallHasDomain(t *testing.T) {
	set := map[string]struct{}{"acme.sip.example.com": {}}
	if !liveCallHasDomain(LiveCall{Domains: []string{"x.com", "ACME.sip.example.com"}}, set) {
		t.Error("expected case-insensitive domain match")
	}
	if liveCallHasDomain(LiveCall{Domains: []string{"other.com"}}, set) {
		t.Error("expected no match")
	}
	if liveCallHasDomain(LiveCall{}, set) {
		t.Error("empty domains should not match")
	}
}
