package freeswitch

import (
	"strings"
	"testing"
)

func TestIsEmergencyNumber(t *testing.T) {
	emergency := []string{"911", "933"}
	for _, d := range emergency {
		if !isEmergencyNumber(d) {
			t.Errorf("isEmergencyNumber(%q) = false, want true", d)
		}
	}
	notEmergency := []string{"", "9", "91", "9110", "1911", "411", "*911", "6501", "+18005551212"}
	for _, d := range notEmergency {
		if isEmergencyNumber(d) {
			t.Errorf("isEmergencyNumber(%q) = true, want false", d)
		}
	}
}

func TestBuildEmergencyActions(t *testing.T) {
	actions := buildEmergencyActions("cc-main", "6501", "123 Main St, Suite 4, Austin, TX, 78701, US")

	var sb strings.Builder
	var bridgeData string
	for _, a := range actions {
		sb.WriteString(a.App + "=" + a.Data + "\n")
		if a.App == "bridge" {
			bridgeData = a.Data
		}
	}
	flat := sb.String()

	// Bridges the literal 911 out through the carrier gateway — NOT an E.164 number.
	if want := "sofia/gateway/cc-main/911"; bridgeData != want {
		t.Errorf("bridge data = %q, want %q", bridgeData, want)
	}
	for _, want := range []string{
		"x_emergency=true",
		"x_e911_extension=6501",
		"x_e911_address=123 Main St, Suite 4, Austin, TX, 78701, US",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("emergency actions missing %q\n--- got ---\n%s", want, flat)
		}
	}
}

func TestBuildEmergencyActionsNoAddress(t *testing.T) {
	// An unassigned extension still routes — the address var is just empty.
	actions := buildEmergencyActions("gw1", "1000", "")
	var bridgeData, addr string
	hasEmergency := false
	for _, a := range actions {
		switch a.App {
		case "bridge":
			bridgeData = a.Data
		}
		if a.Data == "x_emergency=true" {
			hasEmergency = true
		}
		if strings.HasPrefix(a.Data, "x_e911_address=") {
			addr = a.Data
		}
	}
	if bridgeData != "sofia/gateway/gw1/911" {
		t.Errorf("bridge data = %q, want sofia/gateway/gw1/911", bridgeData)
	}
	if !hasEmergency {
		t.Error("emergency actions must stamp x_emergency=true even with no address")
	}
	if addr != "x_e911_address=" {
		t.Errorf("address var = %q, want empty x_e911_address=", addr)
	}
}
