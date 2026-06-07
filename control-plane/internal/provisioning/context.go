package provisioning

import (
	"strings"
	"time"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// RenderContext is the data structure exposed to every per-vendor template.
// Keep field names stable — templates depend on them.
type RenderContext struct {
	Now       time.Time
	MAC       string // colon form: aa:bb:cc:dd:ee:ff
	MACPlain  string // no separators, lowercase: aabbccddeeff
	MACUpper  string // no separators, uppercase: AABBCCDDEEFF
	Vendor    string
	Model     string
	Firmware  string
	Label     string
	Tenant    TenantCtx
	Lines     []LineCtx
	SIP       SIPCtx
	Provision ProvisionCtx
	// Paging lists the multicast paging groups this device should listen on
	// (PTT increment 3). Empty unless the device's extensions belong to one or
	// more `multicast`-mode paging groups with an address configured.
	Paging []PagingMulticastCtx
}

// PagingMulticastCtx describes one multicast listen address for a phone.
type PagingMulticastCtx struct {
	Channel int    // 1-based listen slot
	Label   string // group name (shown on the phone)
	Addr    string // multicast IP
	Port    int    // multicast port
}

type TenantCtx struct {
	ID   string
	Slug string
	Name string
}

type LineCtx struct {
	LineNumber  int
	Label       string
	Extension   string
	Username    string
	Password    string
	DisplayName string
	VoicemailOn bool
	SIPDomain   string // realm
}

type SIPCtx struct {
	Proxy       string // outbound proxy host
	ProxyPort   int
	Transport   string // udp / tcp / tls
	RegisterExp int    // seconds
	Codecs      []string
}

type ProvisionCtx struct {
	PublicHost string
	Token      string
}

// buildContext converts a store.DeviceConfig into the template-facing
// RenderContext, mixing in environment-wide SIP info.
func buildContext(cfg *store.DeviceConfig, sip SIPCtx, prov ProvisionCtx) RenderContext {
	plain := strings.ReplaceAll(cfg.Device.MAC, ":", "")
	plain = strings.ToLower(plain)
	upper := strings.ToUpper(plain)

	lines := make([]LineCtx, 0, len(cfg.Lines))
	for _, l := range cfg.Lines {
		displayName := l.DisplayName
		if displayName == "" {
			displayName = l.Extension
		}
		lines = append(lines, LineCtx{
			LineNumber:  l.LineNumber,
			Label:       firstNonEmpty(l.Label, l.Extension),
			Extension:   l.Extension,
			Username:    l.SIPUsername,
			Password:    l.SIPPassword,
			DisplayName: displayName,
			VoicemailOn: l.VoicemailOn,
			SIPDomain:   l.SIPDomain,
		})
	}

	return RenderContext{
		Now:      time.Now().UTC(),
		MAC:      cfg.Device.MAC,
		MACPlain: plain,
		MACUpper: upper,
		Vendor:   cfg.Device.Vendor,
		Model:    cfg.Device.Model,
		Firmware: cfg.Device.Firmware,
		Label:    cfg.Device.Label,
		Tenant: TenantCtx{
			ID: cfg.Tenant.ID.String(), Slug: cfg.Tenant.Slug, Name: cfg.Tenant.Name,
		},
		Lines:     lines,
		SIP:       sip,
		Provision: prov,
	}
}

// pagingMulticastCtx converts multicast paging groups into per-channel listen
// entries for the templates, skipping any without an address.
func pagingMulticastCtx(groups []store.PagingGroup) []PagingMulticastCtx {
	out := make([]PagingMulticastCtx, 0, len(groups))
	ch := 1
	for _, g := range groups {
		if g.MulticastAddr == "" {
			continue
		}
		out = append(out, PagingMulticastCtx{
			Channel: ch,
			Label:   g.Name,
			Addr:    g.MulticastAddr,
			Port:    g.MulticastPort,
		})
		ch++
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
