//go:build integration

// End-to-end test for the xml_curl dialplan handler — the control-plane acting
// as FreeSWITCH's routing brain. Builds a real request like FS sends on each
// call and asserts the returned dialplan XML. Run via `go test -tags=integration`.
package freeswitch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

func dpStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping dialplan integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return &store.Store{DB: pool}
}

func dpSeed(t *testing.T, s *store.Store) (tenantID uuid.UUID, domain string, ext *store.Extension) {
	t.Helper()
	ctx := context.Background()
	ten, err := s.CreateTenant(ctx, "dp-"+uuid.NewString()[:8], "DP IT")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	t.Cleanup(func() { _, _ = s.DB.Exec(context.Background(), "DELETE FROM tenants WHERE id=$1", ten.ID) })
	domain = "dp-" + uuid.NewString()[:8] + ".sip.local"
	sd, err := s.CreateSIPDomain(ctx, ten.ID, domain, true)
	if err != nil {
		t.Fatalf("CreateSIPDomain: %v", err)
	}
	ext, err = s.CreateExtension(ctx, ten.ID, sd.ID, "6501", "6501", "pw", "Desk")
	if err != nil {
		t.Fatalf("CreateExtension: %v", err)
	}
	return ten.ID, domain, ext
}

// dialplanXML drives the handler with a FreeSWITCH-style request.
func dialplanXML(t *testing.T, h *Handler, dest, domain string) (int, string) {
	t.Helper()
	form := url.Values{
		"section":                            {"dialplan"},
		"destination_number":                 {dest},
		"context":                            {"default"},
		"variable_sip_h_X-Sip-Tenant-Domain": {domain},
		"Caller-Caller-ID-Number":            {"1000"},
	}
	req := httptest.NewRequest("POST", "/v1/freeswitch/dialplan", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestDialplanRoutesToExtension(t *testing.T) {
	s := dpStore(t)
	_, domain, _ := dpSeed(t, s)
	h := NewHandler(s, "kam.example:5060", nil)

	code, xml := dialplanXML(t, h, "6501", domain)
	if code != http.StatusOK {
		t.Fatalf("dialplan status = %d", code)
	}
	for _, want := range []string{
		`application="bridge"`,
		"sofia/internal/sip:6501@" + domain,
		"fs_path=sip:kam.example:5060",
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("extension dialplan missing %q\n%s", want, xml)
		}
	}
}

func TestDialplanRoutesToRingGroup(t *testing.T) {
	s := dpStore(t)
	ctx := context.Background()
	tid, domain, ext := dpSeed(t, s)

	rg, err := s.CreateRingGroup(ctx, store.CreateRingGroupInput{
		TenantID: tid, Extension: "600", Name: "Sales", Strategy: "simultaneous",
	})
	if err != nil {
		t.Fatalf("CreateRingGroup: %v", err)
	}
	if _, err := s.AddRingGroupMember(ctx, store.AddRingGroupMemberInput{RingGroupID: rg.ID, ExtensionID: ext.ID}); err != nil {
		t.Fatalf("AddRingGroupMember: %v", err)
	}

	h := NewHandler(s, "kam.example:5060", nil)
	code, xml := dialplanXML(t, h, "600", domain)
	if code != http.StatusOK {
		t.Fatalf("ring group dialplan status = %d", code)
	}
	for _, want := range []string{`application="bridge"`, "sip:6501@" + domain} {
		if !strings.Contains(xml, want) {
			t.Errorf("ring-group dialplan missing %q\n%s", want, xml)
		}
	}
}

func TestDialplanRoutesToQueue(t *testing.T) {
	s := dpStore(t)
	ctx := context.Background()
	tid, domain, _ := dpSeed(t, s)

	q, err := s.CreateQueue(ctx, store.CreateQueueInput{TenantID: tid, Extension: "700", Name: "Support"})
	if err != nil {
		t.Fatalf("CreateQueue: %v", err)
	}
	h := NewHandler(s, "kam.example:5060", nil)
	code, xml := dialplanXML(t, h, "700", domain)
	if code != http.StatusOK {
		t.Fatalf("queue dialplan status = %d", code)
	}
	for _, want := range []string{`application="callcenter"`, q.ID.String()} {
		if !strings.Contains(xml, want) {
			t.Errorf("queue dialplan missing %q\n%s", want, xml)
		}
	}
}

func TestDialplanRoutesQueueCallbackOffer(t *testing.T) {
	s := dpStore(t)
	ctx := context.Background()
	tid, domain, _ := dpSeed(t, s)

	q, err := s.CreateQueue(ctx, store.CreateQueueInput{TenantID: tid, Extension: "700", Name: "Support"})
	if err != nil {
		t.Fatalf("CreateQueue: %v", err)
	}

	h := NewHandler(s, "kam.example:5060", nil)
	// *8<queue extension> reaches the callback offer for queue 700.
	code, xml := dialplanXMLFromCaller(t, h, "*8700", domain, "+14155550123")
	if code != http.StatusOK {
		t.Fatalf("callback offer dialplan status = %d", code)
	}
	for _, want := range []string{
		`application="play_and_get_digits"`, // the "press 1" offer
		"x_queue_callback_offer=true",
		"x_queue_id=" + q.ID.String(),
		"700 XML default", // non-opt-in transfers back into the queue
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("callback offer dialplan missing %q\n%s", want, xml)
		}
	}

	// Reaching the offer recorded a pending callback for the caller.
	pending, err := s.ListPendingQueueCallbacks(ctx, tid)
	if err != nil {
		t.Fatalf("ListPendingQueueCallbacks: %v", err)
	}
	if len(pending) != 1 || pending[0].CallerNumber != "+14155550123" {
		t.Fatalf("expected 1 pending callback for the caller, got %+v", pending)
	}
}

func TestDialplanRoutesToIVR(t *testing.T) {
	s := dpStore(t)
	ctx := context.Background()
	tid, domain, _ := dpSeed(t, s)

	ivr, err := s.CreateIVR(ctx, store.CreateIVRInput{TenantID: tid, Name: "Menu", Extension: "900"})
	if err != nil {
		t.Fatalf("CreateIVR: %v", err)
	}
	h := NewHandler(s, "kam.example:5060", nil)
	code, xml := dialplanXML(t, h, "900", domain)
	if code != http.StatusOK {
		t.Fatalf("ivr dialplan status = %d", code)
	}
	for _, want := range []string{`application="ivr"`, ivr.ID.String()} {
		if !strings.Contains(xml, want) {
			t.Errorf("ivr dialplan missing %q\n%s", want, xml)
		}
	}
}

func TestDialplanRoutesRoomNumberToConference(t *testing.T) {
	s := dpStore(t)
	ctx := context.Background()
	tid, domain, _ := dpSeed(t, s)

	room, err := s.CreateConferenceRoom(ctx, store.CreateConferenceRoomInput{
		TenantID: tid, Name: "All hands", Extension: "8100",
		PIN: "1234", ModeratorPIN: "9999", AnnounceCount: true,
	})
	if err != nil {
		t.Fatalf("CreateConferenceRoom: %v", err)
	}

	h := NewHandler(s, "kam.example:5060", nil)
	code, xml := dialplanXML(t, h, "8100", domain)
	if code != http.StatusOK {
		t.Fatalf("conference dialplan status = %d", code)
	}
	for _, want := range []string{
		`application="conference"`,
		`application="play_and_get_digits"`, // PIN prompt (both PINs set)
		tid.String() + "_8100@default",      // per-tenant room in the default profile
		"x_conference_room_id=" + room.ID.String(),
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("conference dialplan missing %q\n%s", want, xml)
		}
	}
}

func TestDialplanRoutesParkFeatureCodeAndSlot(t *testing.T) {
	s := dpStore(t)
	ctx := context.Background()
	tid, domain, _ := dpSeed(t, s)

	lot, err := s.CreateParkLot(ctx, store.CreateParkLotInput{
		TenantID: tid, Name: "Front desk", FeatureCode: "*68",
		SlotStart: 700, SlotEnd: 779,
	})
	if err != nil {
		t.Fatalf("CreateParkLot: %v", err)
	}

	h := NewHandler(s, "kam.example:5060", nil)

	// Feature code → park (auto-assign over the slot range).
	code, xml := dialplanXML(t, h, "*68", domain)
	if code != http.StatusOK {
		t.Fatalf("park dialplan status = %d", code)
	}
	for _, want := range []string{
		`application="valet_park"`,
		"auto in 700 779",
		"x_park_lot_id=" + lot.ID.String(),
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("park dialplan missing %q\n%s", want, xml)
		}
	}

	// Slot number → retrieve (explicit slot, no auto).
	code, xml = dialplanXML(t, h, "703", domain)
	if code != http.StatusOK {
		t.Fatalf("park-retrieve dialplan status = %d", code)
	}
	if !strings.Contains(xml, `application="valet_park"`) {
		t.Errorf("park-retrieve dialplan missing valet_park\n%s", xml)
	}
	if strings.Contains(xml, "auto in") {
		t.Errorf("park-retrieve should target an explicit slot, not auto:\n%s", xml)
	}
	if !strings.Contains(xml, " 703") {
		t.Errorf("park-retrieve dialplan missing slot 703\n%s", xml)
	}
}

// dialplanXMLFromCaller is like dialplanXML but lets the test set the calling
// extension number (Caller-Caller-ID-Number) so emergency resolution can find
// the right extension + dispatchable location.
func dialplanXMLFromCaller(t *testing.T, h *Handler, dest, domain, caller string) (int, string) {
	t.Helper()
	form := url.Values{
		"section":                            {"dialplan"},
		"destination_number":                 {dest},
		"context":                            {"default"},
		"variable_sip_h_X-Sip-Tenant-Domain": {domain},
		"Caller-Caller-ID-Number":            {caller},
	}
	req := httptest.NewRequest("POST", "/v1/freeswitch/dialplan", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestDialplanRoutesEmergencyToCarrierWith911(t *testing.T) {
	s := dpStore(t)
	ctx := context.Background()
	tid, domain, ext := dpSeed(t, s)

	// A tenant carrier so the emergency call has a gateway to bridge out on.
	carriers, err := s.ListCarriers(ctx)
	if err != nil {
		t.Fatalf("ListCarriers: %v", err)
	}
	if len(carriers) == 0 {
		t.Skip("no seeded carriers")
	}
	gw := "gw_" + uuid.NewString()[:8]
	if _, err := s.CreateCarrierAccount(ctx, store.CreateCarrierAccountInput{
		TenantID: &tid, CarrierID: carriers[0].ID, Name: "trunk",
		SIPUsername: "u", SIPPassword: "p", FSGatewayName: gw, Register: true,
	}); err != nil {
		t.Fatalf("CreateCarrierAccount: %v", err)
	}

	// A dispatchable location assigned to the calling extension (6501).
	loc, err := s.CreateE911Location(ctx, store.CreateE911LocationInput{
		TenantID: tid, Label: "HQ", Street: "123 Main St",
		City: "Austin", Region: "TX", PostalCode: "78701",
	})
	if err != nil {
		t.Fatalf("CreateE911Location: %v", err)
	}
	if err := s.SetExtensionE911Location(ctx, tid, ext.ID, &loc.ID); err != nil {
		t.Fatalf("SetExtensionE911Location: %v", err)
	}

	h := NewHandler(s, "kam.example:5060", nil)
	code, xml := dialplanXMLFromCaller(t, h, "911", domain, "6501")
	if code != http.StatusOK {
		t.Fatalf("emergency dialplan status = %d", code)
	}
	for _, want := range []string{
		`application="bridge"`,
		"sofia/gateway/" + gw + "/911",
		"x_emergency=true",
		"x_e911_extension=6501",
		"x_e911_address=123 Main St, Austin, TX, 78701, US",
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("emergency dialplan missing %q\n%s", want, xml)
		}
	}
	// 911 must NOT be normalized to E.164 — no "+1" prefix on the bridge target.
	if strings.Contains(xml, "sofia/gateway/"+gw+"/1911") {
		t.Errorf("911 should be dialed verbatim, not normalized:\n%s", xml)
	}
}

func TestDialplanRoutesPageCodeToConference(t *testing.T) {
	s := dpStore(t)
	ctx := context.Background()
	tid, domain, ext := dpSeed(t, s)

	pg, err := s.CreatePagingGroup(ctx, store.CreatePagingGroupInput{
		TenantID: tid, Name: "All", Extension: "800", Mode: "fs_conference",
	})
	if err != nil {
		t.Fatalf("CreatePagingGroup: %v", err)
	}
	if _, err := s.AddPagingMember(ctx, pg.ID, ext.ID); err != nil {
		t.Fatalf("AddPagingMember: %v", err)
	}

	h := NewHandler(s, "kam.example:5060", nil)
	code, xml := dialplanXML(t, h, "800", domain)
	if code != http.StatusOK {
		t.Fatalf("paging dialplan status = %d", code)
	}
	for _, want := range []string{
		`application="conference"`,
		"@paging",
		"conference_set_auto_outcall",
		"sofia/internal/sip:6501@" + domain, // the member is auto-outcalled
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("paging dialplan missing %q\n%s", want, xml)
		}
	}
}
