package freeswitch

import "testing"

func TestParseActiveCalls(t *testing.T) {
	// Two legs of one bridged call (shared call_uuid) + one single-leg call.
	body := `{"row_count":3,"rows":[
	  {"uuid":"aaa","direction":"inbound","created_epoch":"100","cid_num":"101","cid_name":"Alice","callee_num":"102","callstate":"ACTIVE","call_uuid":"call-1","presence_id":"101@acme.sip.example.com"},
	  {"uuid":"bbb","direction":"outbound","created_epoch":"101","cid_num":"101","cid_name":"Alice","dest":"102","callstate":"ACTIVE","call_uuid":"call-1","presence_id":"102@acme.sip.example.com"},
	  {"uuid":"ccc","direction":"inbound","created_epoch":"200","cid_num":"+14155551234","cid_name":"Ext Caller","callee_num":"500","callstate":"RINGING","call_uuid":"call-2","presence_id":"500@other.sip.example.com"}
	]}`
	calls := parseActiveCalls(body)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	// Sorted most-recent first → call-2 (epoch 200) before call-1 (epoch 100).
	if calls[0].CallUUID != "call-2" {
		t.Errorf("expected call-2 first, got %s", calls[0].CallUUID)
	}
	c1 := calls[1]
	if c1.CallUUID != "call-1" || c1.CallerNum != "101" || c1.CalleeNum != "102" {
		t.Errorf("call-1 mismatch: %+v", c1)
	}
	if c1.KillUUID != "aaa" {
		t.Errorf("expected A-leg uuid aaa as kill target, got %s", c1.KillUUID)
	}
	if c1.StartEpoch != 100 {
		t.Errorf("expected earliest epoch 100, got %d", c1.StartEpoch)
	}
	// Domain scoping: call-1 belongs to acme; call-2 to other.
	acme := map[string]struct{}{"acme.sip.example.com": {}}
	if !c1.HasDomain(acme) {
		t.Error("call-1 should match acme domain")
	}
	if calls[0].HasDomain(acme) {
		t.Error("call-2 should NOT match acme domain")
	}
}

func TestDomainOf(t *testing.T) {
	cases := []struct {
		row  map[string]any
		want string
	}{
		{map[string]any{"presence_id": "101@acme.sip.example.com"}, "acme.sip.example.com"},
		{map[string]any{"sip_from_host": "carrier.example.net"}, "carrier.example.net"},
		{map[string]any{"sip_to_host": "to.example.org"}, "to.example.org"},
		{map[string]any{}, ""},
		{map[string]any{"presence_id": ""}, ""},
	}
	for _, c := range cases {
		if got := domainOf(c.row); got != c.want {
			t.Errorf("domainOf(%v)=%q want %q", c.row, got, c.want)
		}
	}
}

func TestParseActiveCallsEmpty(t *testing.T) {
	for _, body := range []string{`{"row_count":0}`, ``, `not json`} {
		if got := parseActiveCalls(body); got != nil {
			t.Errorf("parseActiveCalls(%q) = %v, want nil", body, got)
		}
	}
}
