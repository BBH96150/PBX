//go:build integration

package freeswitch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// inboundDialplanXML drives the handler with a FreeSWITCH public-context
// (inbound PSTN) request: a DID destination + a calling caller-ID.
func inboundDialplanXML(t *testing.T, h *Handler, did, caller string) (int, string) {
	t.Helper()
	form := url.Values{
		"section":                 {"dialplan"},
		"destination_number":      {did},
		"context":                 {"public"},
		"Caller-Caller-ID-Number": {caller},
	}
	req := httptest.NewRequest("POST", "/v1/freeswitch/dialplan", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestDialplanScreensBlockedInboundCaller(t *testing.T) {
	s := dpStore(t)
	ctx := context.Background()
	tid, _, ext := dpSeed(t, s)

	// Provision a DID that routes to the seeded extension.
	carriers, err := s.ListCarriers(ctx)
	if err != nil {
		t.Fatalf("ListCarriers: %v", err)
	}
	if len(carriers) == 0 {
		t.Skip("no seeded carriers")
	}
	// A guaranteed-valid +1NXXNXXXXXX shape with a unique line number.
	did := "+1555200" + uuidDigits(4)
	if _, err := s.CreateDID(ctx, store.CreateDIDInput{
		TenantID: tid, CarrierID: carriers[0].ID, E164: did,
		DestinationKind: "extension", DestinationID: ext.ID,
	}); err != nil {
		t.Fatalf("CreateDID: %v", err)
	}

	caller := "+14155559876"
	if _, err := s.CreateBlockedNumber(ctx, store.CreateBlockedNumberInput{
		TenantID: tid, Number: caller, Label: "spam",
	}); err != nil {
		t.Fatalf("CreateBlockedNumber: %v", err)
	}

	h := NewHandler(s, "kam.example:5060", nil)

	// Blocked caller → reject dialplan (no bridge).
	code, xml := inboundDialplanXML(t, h, did, caller)
	if code != http.StatusOK {
		t.Fatalf("blocked inbound dialplan status = %d", code)
	}
	for _, want := range []string{"x_blocked=true", `application="hangup"`, "CALL_REJECTED"} {
		if !strings.Contains(xml, want) {
			t.Errorf("blocked inbound dialplan missing %q\n%s", want, xml)
		}
	}
	if strings.Contains(xml, `application="bridge"`) {
		t.Errorf("blocked inbound call must not bridge:\n%s", xml)
	}

	// A non-blocked caller routes normally (bridges to the extension).
	code, xml = inboundDialplanXML(t, h, did, "+14155550000")
	if code != http.StatusOK {
		t.Fatalf("allowed inbound dialplan status = %d", code)
	}
	if !strings.Contains(xml, `application="bridge"`) {
		t.Errorf("non-blocked inbound call should bridge:\n%s", xml)
	}
	if strings.Contains(xml, "x_blocked=true") {
		t.Errorf("non-blocked call should not be marked blocked:\n%s", xml)
	}
}

// uuidDigits returns n digits derived from a fresh UUID (for building unique
// test DIDs that satisfy the +1NXXNXXXXXX shape).
func uuidDigits(n int) string {
	var b strings.Builder
	for _, r := range uuid.NewString() {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
			if b.Len() == n {
				break
			}
		}
	}
	for b.Len() < n {
		b.WriteByte('0')
	}
	return b.String()
}
