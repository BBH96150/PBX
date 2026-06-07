package provisioning

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

func TestLoadAndRenderTemplates(t *testing.T) {
	reg, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	if len(reg.tmpls) == 0 {
		t.Fatal("no templates loaded")
	}

	ctx := RenderContext{
		Now:      time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC),
		MAC:      "00:15:65:ab:cd:ef",
		MACPlain: "001565abcdef",
		MACUpper: "001565ABCDEF",
		Vendor:   "yealink",
		Model:    "t46u",
		Label:    "Reception",
		Tenant:   TenantCtx{ID: uuid.NewString(), Slug: "acme", Name: "Acme Corp"},
		Lines: []LineCtx{
			{
				LineNumber: 1, Label: "101", Extension: "101",
				Username: "101", Password: "s3cret",
				DisplayName: "Alice", SIPDomain: "acme.sip.local",
			},
			{
				LineNumber: 2, Label: "102", Extension: "102",
				Username: "102", Password: "s3cret2",
				DisplayName: "Bob", SIPDomain: "acme.sip.local",
				VoicemailOn: true,
			},
		},
		SIP: SIPCtx{
			Proxy: "sip.example.local", ProxyPort: 5060, Transport: "udp",
			RegisterExp: 600, Codecs: []string{"OPUS", "G722", "PCMU", "PCMA"},
		},
		Provision: ProvisionCtx{
			PublicHost: "provision.example.local",
			Token:      "deadbeef",
		},
	}

	wantInName := []string{
		"polycom/master.cfg",
		"yealink/account.cfg",
		"grandstream/grp.xml",
	}
	for _, name := range wantInName {
		var buf bytes.Buffer
		if err := reg.execute(&buf, name, ctx); err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		out := buf.String()
		if !strings.Contains(out, "101") || !strings.Contains(out, "acme.sip.local") {
			t.Errorf("%s output missing expected substrings; got:\n%s", name, out)
		}
	}
}

func TestPagingMulticastCtx(t *testing.T) {
	// Skips groups without an address; assigns sequential channels.
	got := pagingMulticastCtx([]store.PagingGroup{
		{Name: "All staff", MulticastAddr: "224.0.1.116", MulticastPort: 5000},
		{Name: "No addr", MulticastAddr: "", MulticastPort: 0},
		{Name: "Kitchen", MulticastAddr: "224.0.1.117", MulticastPort: 5001},
	})
	if len(got) != 2 {
		t.Fatalf("want 2 channels (addr-less skipped), got %d: %+v", len(got), got)
	}
	if got[0].Channel != 1 || got[1].Channel != 2 {
		t.Errorf("channels not sequential: %+v", got)
	}
	if got[0].Addr != "224.0.1.116" || got[1].Label != "Kitchen" {
		t.Errorf("unexpected mapping: %+v", got)
	}
}

func TestYealinkRendersMulticast(t *testing.T) {
	reg, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	ctx := RenderContext{
		Now:    time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
		Vendor: "yealink",
		Tenant: TenantCtx{ID: uuid.NewString(), Slug: "acme"},
		Lines:  []LineCtx{{LineNumber: 1, Extension: "101", Username: "101", SIPDomain: "acme.sip.local"}},
		SIP:    SIPCtx{Proxy: "p", ProxyPort: 5060, Transport: "udp", RegisterExp: 600},
		Paging: []PagingMulticastCtx{{Channel: 1, Label: "All staff", Addr: "224.0.1.116", Port: 5000}},
	}
	var buf bytes.Buffer
	if err := reg.execute(&buf, "yealink/account.cfg", ctx); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"multicast.listen_address.1.ip_address = 224.0.1.116:5000",
		"multicast.listen_address.1.label",
		"All staff",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("yealink output missing %q\n%s", want, out)
		}
	}

	// Without paging groups, no multicast block is emitted.
	ctx.Paging = nil
	buf.Reset()
	_ = reg.execute(&buf, "yealink/account.cfg", ctx)
	if strings.Contains(buf.String(), "multicast.listen_address") {
		t.Error("multicast block emitted with no paging groups")
	}
}
