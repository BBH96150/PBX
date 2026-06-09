//go:build integration

package provisioning

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// Validates the full multicast-paging provisioning chain: a multicast paging
// group containing a device's extension → ListMulticastPagingForExtensions →
// the render context's Paging slice → the Yealink multicast.listen_address
// block in the rendered config.
func TestProvisioningEmitsMulticastPagingForMember(t *testing.T) {
	s := provStore(t)
	ctx := context.Background()

	ten, err := s.CreateTenant(ctx, "mc-"+uuid.NewString()[:8], "MC IT")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	t.Cleanup(func() { _, _ = s.DB.Exec(context.Background(), "DELETE FROM tenants WHERE id=$1", ten.ID) })

	domain := "mc-" + uuid.NewString()[:8] + ".sip.local"
	sd, err := s.CreateSIPDomain(ctx, ten.ID, domain, true)
	if err != nil {
		t.Fatalf("CreateSIPDomain: %v", err)
	}
	ext, err := s.CreateExtension(ctx, ten.ID, sd.ID, "7801", "7801", "pw", "Desk")
	if err != nil {
		t.Fatalf("CreateExtension: %v", err)
	}

	mac := "001565ddeeff"
	if _, err := s.CreateDevice(ctx, ten.ID, mac, "yealink", "t46u", "Desk"); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	if _, err := s.CreateDeviceLine(ctx, mac, 1, ext.ID, ""); err != nil {
		t.Fatalf("CreateDeviceLine: %v", err)
	}

	// A multicast paging group containing this extension.
	pg, err := s.CreatePagingGroup(ctx, store.CreatePagingGroupInput{
		TenantID: ten.ID, Name: "All staff", Mode: "multicast",
		MulticastAddr: "224.0.1.116", MulticastPort: 5000,
	})
	if err != nil {
		t.Fatalf("CreatePagingGroup: %v", err)
	}
	if _, err := s.AddPagingMember(ctx, pg.ID, ext.ID); err != nil {
		t.Fatalf("AddPagingMember: %v", err)
	}

	code, body := provGet(t, s, "/"+mac+".cfg")
	if code != http.StatusOK {
		t.Fatalf("GET /%s.cfg = %d\n%s", mac, code, body)
	}
	want := "multicast.listen_address.1.ip_address = 224.0.1.116:5000"
	if !strings.Contains(body, want) {
		t.Errorf("multicast block missing %q\n%s", want, body)
	}
}
