package freeswitch

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// flatten joins actions into "app=data\n" lines and pulls out the conference
// app's data for targeted assertions.
func flattenConf(actions []dialplanAction) (flat, confData string) {
	var sb strings.Builder
	for _, a := range actions {
		sb.WriteString(a.App + "=" + a.Data + "\n")
		if a.App == "conference" {
			confData = a.Data
		}
	}
	return sb.String(), confData
}

func TestBuildConferenceActionsNoPIN(t *testing.T) {
	tid := uuid.New()
	room := &store.ConferenceRoom{
		ID: uuid.New(), TenantID: tid, Extension: "8100", Name: "Standup",
		AnnounceCount: true,
	}
	actions := buildConferenceActions(room, "internal")
	flat, confData := flattenConf(actions)

	for _, want := range []string{
		"answer=",
		"x_conference_room_id=" + room.ID.String(),
		"x_tenant_id=" + tid.String(),
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("actions missing %q\n--- got ---\n%s", want, flat)
		}
	}
	// Joins the per-tenant/room conference in the default profile.
	if want := tid.String() + "_8100@default"; !strings.Contains(confData, want) {
		t.Errorf("conference data = %q, want it to contain %q", confData, want)
	}
	// No PIN → no PIN prompt, no flags.
	if strings.Contains(flat, "play_and_get_digits") {
		t.Errorf("no-PIN room should not prompt for a PIN:\n%s", flat)
	}
	if strings.Contains(confData, "flags{") {
		t.Errorf("no-PIN room should join with no flags: %s", confData)
	}
}

func TestBuildConferenceActionsMemberPINOnly(t *testing.T) {
	room := &store.ConferenceRoom{
		ID: uuid.New(), TenantID: uuid.New(), Extension: "8101",
		PIN: "1234", AnnounceCount: true,
	}
	flat, confData := flattenConf(buildConferenceActions(room, "internal"))
	if !strings.Contains(flat, "play_and_get_digits") {
		t.Errorf("member-PIN room should prompt for a PIN:\n%s", flat)
	}
	if !strings.Contains(flat, "conf_pin 1234") {
		t.Errorf("PIN prompt should match the member PIN:\n%s", flat)
	}
	// Member PIN only → join as a plain member, no flags.
	if strings.Contains(confData, "flags{") {
		t.Errorf("member-only PIN should join without moderator flags: %s", confData)
	}
}

func TestBuildConferenceActionsModeratorPIN(t *testing.T) {
	room := &store.ConferenceRoom{
		ID: uuid.New(), TenantID: uuid.New(), Extension: "8102",
		PIN: "1234", ModeratorPIN: "9999", AnnounceCount: true,
		Record: true, MaxMembers: 10,
	}
	flat, confData := flattenConf(buildConferenceActions(room, "internal"))

	// Accepts either PIN.
	if !strings.Contains(flat, "conf_pin 1234|9999") {
		t.Errorf("dual-PIN room should accept either PIN:\n%s", flat)
	}
	// Moderator flags selected at runtime via the moderator PIN.
	if !strings.Contains(flat, "moderator|endconf") {
		t.Errorf("dual-PIN room should be able to join as moderator:\n%s", flat)
	}
	if !strings.Contains(confData, "+flags{${conf_flags}}") {
		t.Errorf("dual-PIN conference should use the runtime-selected flags: %s", confData)
	}
	// Record + max members applied.
	if !strings.Contains(flat, "record_session=") {
		t.Errorf("record=true should add record_session:\n%s", flat)
	}
	if !strings.Contains(flat, "conference_max_members=10") {
		t.Errorf("max_members should cap the room:\n%s", flat)
	}
}

func TestBuildConferenceActionsModeratorPINOnly(t *testing.T) {
	room := &store.ConferenceRoom{
		ID: uuid.New(), TenantID: uuid.New(), Extension: "8103",
		ModeratorPIN: "9999", AnnounceCount: true,
	}
	flat, confData := flattenConf(buildConferenceActions(room, "internal"))
	if !strings.Contains(flat, "conf_pin 9999") {
		t.Errorf("moderator-only PIN should prompt for the moderator PIN:\n%s", flat)
	}
	if !strings.Contains(confData, "+flags{moderator|endconf}") {
		t.Errorf("moderator-only PIN should join as moderator: %s", confData)
	}
}
