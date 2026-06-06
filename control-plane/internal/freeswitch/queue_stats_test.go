package freeswitch

import "testing"

func TestParseCCListAgents(t *testing.T) {
	// Verbatim from live FS 5.5.2 `callcenter_config agent list` (one agent).
	body := "name|instance_id|uuid|type|contact|status|state|max_no_answer|wrap_up_time|reject_delay_time|busy_delay_time|no_answer_delay_time|last_bridge_start|last_bridge_end|last_offered_call|last_status_change|no_answer_count|calls_answered|talk_time|ready_time|external_calls_count\n" +
		"agent_abc|single_box||callback|error/user_busy|Available|Waiting|0|0|0|0|0|0|0|0|0|0|0|0|0|0\n+OK\n"
	rows := parseCCList(body)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0]["name"] != "agent_abc" || rows[0]["status"] != "Available" || rows[0]["state"] != "Waiting" {
		t.Errorf("bad parse: %+v", rows[0])
	}
}

func TestParseCCListEmpty(t *testing.T) {
	// Empty list prints only "+OK" (no header) — header-only prints no rows.
	for _, b := range []string{"+OK\n", "", "name|strategy|moh_sound\n+OK\n"} {
		if got := parseCCList(b); len(got) != 0 {
			t.Errorf("parseCCList(%q) = %v, want 0 rows", b, got)
		}
	}
}

func TestParseCCListMembers(t *testing.T) {
	body := "queue|system|uuid|session_uuid|cid_number|cid_name|system_epoch|joined_epoch|rejoined_epoch|bridge_epoch|abandoned_epoch|base_score|skill_score|serving_agent|serving_system|state\n" +
		"q1|single_box|u1|s1|+14155551234|Dana|100|105|0|0|0|0|0|||Waiting\n+OK\n"
	rows := parseCCList(body)
	if len(rows) != 1 {
		t.Fatalf("expected 1 member, got %d", len(rows))
	}
	m := rows[0]
	if m["cid_number"] != "+14155551234" || m["cid_name"] != "Dana" || m["joined_epoch"] != "105" || m["state"] != "Waiting" {
		t.Errorf("bad member parse: %+v", m)
	}
}
