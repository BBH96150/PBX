package provisioning

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
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
