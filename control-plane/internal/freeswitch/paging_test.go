package freeswitch

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

func TestBuildPagingActions(t *testing.T) {
	gid := uuid.New()
	tid := uuid.New()
	info := &store.PagingRoutingInfo{
		Group: store.PagingGroup{ID: gid, TenantID: tid, Extension: "800", Mode: "fs_conference"},
		Members: []store.PagingGroupMember{
			{SIPUsername: "1001", SIPDomain: "acme.sip.local"},
			{SIPUsername: "1002", SIPDomain: "acme.sip.local"},
		},
	}

	actions := buildPagingActions(info, "kam.example:5060")
	if actions == nil {
		t.Fatal("expected actions, got nil")
	}

	// Flatten to a single string for substring assertions.
	var sb strings.Builder
	var confData, outcallData string
	for _, a := range actions {
		sb.WriteString(a.App + "=" + a.Data + "\n")
		if a.App == "conference" {
			confData = a.Data
		}
		if a.App == "conference_set_auto_outcall" {
			outcallData = a.Data
		}
	}
	flat := sb.String()

	for _, want := range []string{
		"answer=",
		"x_paging_group_id=" + gid.String(),
		"conference_auto_outcall_flags=mute",     // members listen-only
		"sip_auto_answer=true",                   // hands-free auto-answer
		"conference_set_auto_outcall=",
		"paging_" + gid.String() + "@paging",     // dedicated paging profile
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("actions missing %q\n--- got ---\n%s", want, flat)
		}
	}

	// Both members must be auto-outcalled, space-separated, with fs_path routing.
	for _, u := range []string{"sip:1001@acme.sip.local", "sip:1002@acme.sip.local"} {
		if !strings.Contains(outcallData, u) {
			t.Errorf("outcall list missing member %q: %s", u, outcallData)
		}
	}
	if !strings.Contains(outcallData, "fs_path=sip:kam.example:5060") {
		t.Errorf("outcall list missing fs_path: %s", outcallData)
	}
	if !strings.Contains(confData, "endconf") {
		t.Errorf("conference should end when pager leaves: %s", confData)
	}
}

func TestBuildPagingActionsNoMembers(t *testing.T) {
	info := &store.PagingRoutingInfo{
		Group: store.PagingGroup{ID: uuid.New(), TenantID: uuid.New()},
	}
	if got := buildPagingActions(info, "x:5060"); got != nil {
		t.Errorf("expected nil for empty group, got %v", got)
	}
}
