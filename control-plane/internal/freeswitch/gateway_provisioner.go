// Gateway provisioner — Phase 5.1.
//
// Materializes per-tenant carrier_accounts rows as FreeSWITCH gateway XML
// in a shared directory (mounted into both control-plane and FreeSWITCH),
// then asks Sofia to rescan the external profile so the gateways register.
//
// File naming: <fs_gateway_name>.xml — uniquely-constrained at the DB level.
// Old files for deleted/disabled accounts are cleaned up on every sync so the
// directory always mirrors the enabled-true rows.
//
// Triggered by:
//   - Admin creates/edits/deletes a carrier_account (portal handler calls
//     SyncGateways)
//   - Startup (control-plane Run() does an initial sync so a fresh boot
//     reconciles whatever's in the DB)
//
// Falls back gracefully when ESL is disconnected: writes the XML anyway so
// FS will pick it up on next manual rescan or restart.

package freeswitch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// GatewayProvisioner writes per-account XML and asks Sofia to rescan.
type GatewayProvisioner struct {
	store   *store.Store
	esl     *ESLClient
	dir     string // host path that's mounted into both containers
	logDir  string // read-only path to FS log dir (or "" to disable log-grep diagnostics)
	profile string // sofia profile to rescan ("external")
}

func NewGatewayProvisioner(s *store.Store, esl *ESLClient, dir, logDir string) *GatewayProvisioner {
	if dir == "" {
		dir = "/etc/freeswitch/sip_profiles/external/dynamic" // FS container default
	}
	return &GatewayProvisioner{store: s, esl: esl, dir: dir, logDir: logDir, profile: "external"}
}

// SyncGateways writes every enabled carrier_account as an XML file, purges
// stale files, then forces Sofia to drop + reload each managed gateway.
//
// Why kill before rescan: Sofia caches the gateway definition in memory.
// `sofia profile external rescan` only loads NEW gateway files; it does
// NOT re-read changes to existing gateways, and it never re-tries gateways
// that hit FAIL_WAIT with retry=NEVER. The safe pattern is
// `killgw <name>` → wait briefly → `rescan` so the gateway gets re-parsed
// from the (possibly updated) XML file and starts a fresh registration.
//
// Best-effort throughout: if ESL is disconnected we still write the XML so
// the next manual reload or container restart picks it up.
func (p *GatewayProvisioner) SyncGateways(ctx context.Context) error {
	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		return fmt.Errorf("create gateway dir: %w", err)
	}

	accounts, err := p.store.ListAllEnabledCarrierAccounts(ctx)
	if err != nil {
		return fmt.Errorf("list carrier accounts: %w", err)
	}

	// 1. Write every wanted XML file, indexed for stale-cleanup.
	wanted := map[string]bool{}
	gatewayNames := make([]string, 0, len(accounts))
	for _, a := range accounts {
		name := safeFileBase(a.FSGatewayName)
		if name == "" {
			continue
		}
		wanted[name+".xml"] = true
		gatewayNames = append(gatewayNames, a.FSGatewayName)
		xml, err := renderGatewayXML(a)
		if err != nil {
			slog.Warn("gateway XML render failed", "account", a.FSGatewayName, "err", err)
			continue
		}
		path := filepath.Join(p.dir, name+".xml")
		if err := atomicWrite(path, []byte(xml)); err != nil {
			slog.Warn("gateway XML write failed", "path", path, "err", err)
		}
	}

	// 2. Purge stale files (deleted/disabled accounts).
	staleNames := []string{}
	if entries, err := os.ReadDir(p.dir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".xml") {
				continue
			}
			if !wanted[e.Name()] {
				// The gateway name is the filename without .xml.
				staleNames = append(staleNames, strings.TrimSuffix(e.Name(), ".xml"))
				_ = os.Remove(filepath.Join(p.dir, e.Name()))
			}
		}
	}

	// 3. Drop every managed gateway from Sofia's runtime cache so the
	// upcoming rescan picks up any param changes (or completely-new XML
	// for renamed-but-same-file scenarios).
	for _, gw := range append(gatewayNames, staleNames...) {
		if err := p.esl.callAPI(ctx, "sofia profile "+p.profile+" killgw "+gw); err != nil {
			if !errors.Is(err, ErrNotConnected) {
				slog.Warn("sofia killgw failed", "gateway", gw, "err", err)
			}
		}
	}

	// 4. Rescan picks up the (possibly updated) XML files and starts fresh
	// registrations for everything.
	if err := p.esl.callAPI(ctx, "sofia profile "+p.profile+" rescan"); err != nil {
		if !errors.Is(err, ErrNotConnected) {
			slog.Warn("sofia rescan failed", "err", err)
		}
	} else {
		slog.Info("sofia profile reloaded",
			"profile", p.profile,
			"active_gateways", len(gatewayNames),
			"removed_gateways", len(staleNames),
		)
	}
	return nil
}

// GatewayLiveStatus is what the portal renders next to each trunk row.
// Populated by GatewayStatus via a synchronous ESL call.
type GatewayLiveStatus struct {
	Found       bool   // false if Sofia doesn't know about the gateway at all
	State       string // "REGED" | "FAIL_WAIT" | "TRYING" | "UNREGED" | "NOREG" | ...
	Status      string // "UP" | "DOWN"
	PingTime    string // e.g. "12.34"  (ms)
	Uptime      string // e.g. "5m 12s"
	CallsIn     string
	CallsOut    string
	Error       string // populated when ESL isn't connected or the query fails
	LastSIPCode string // e.g. "403" — pulled from FS log when registration failed
	LastSIPMsg  string // e.g. "Incorrect Authentication"
}

// GatewayStatus pulls the current registration state for one gateway by
// parsing `sofia status gateway external::<name>` output. Designed to be
// safe to call every 2-3 seconds from the portal's htmx poller.
//
// When State == FAIL_WAIT we also grep the FS log for the most recent
// "Failed Registration with status <X> [NNN]" line for this gateway, so
// the portal can show "403 Incorrect Authentication" instead of opaque
// "FAIL_WAIT".
func (p *GatewayProvisioner) GatewayStatus(ctx context.Context, gatewayName string) GatewayLiveStatus {
	out, err := p.esl.CallAPISync(ctx, "sofia status gateway "+p.profile+"::"+gatewayName)
	if err != nil {
		return GatewayLiveStatus{Error: err.Error()}
	}
	out = strings.TrimSpace(out)
	if out == "" || strings.Contains(out, "Invalid Gateway") || strings.HasPrefix(out, "-ERR") {
		return GatewayLiveStatus{}
	}
	st := GatewayLiveStatus{Found: true}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := splitStatusLine(line)
		if !ok {
			continue
		}
		switch k {
		case "State":
			st.State = v
		case "Status":
			st.Status = v
		case "PingTime":
			st.PingTime = v
		case "Uptime":
			st.Uptime = v
		case "CallsIN":
			st.CallsIn = v
		case "CallsOUT":
			st.CallsOut = v
		}
	}
	// On failure, dig out the most recent SIP-level reason from FS logs so
	// the portal can be explicit: "403 Incorrect Authentication" beats
	// "FAIL_WAIT" by a mile for someone troubleshooting their CallCentric
	// credentials.
	if st.State == "FAIL_WAIT" || st.State == "" || st.Status == "DOWN" {
		if code, msg := p.lastRegFailure(ctx, gatewayName); code != "" || msg != "" {
			st.LastSIPCode = code
			st.LastSIPMsg = msg
		}
	}
	return st
}

// lastRegFailure reads the FS log file (mounted read-only into the
// control-plane container) and returns the most recent
//   "<gatewayName> Failed Registration with status <msg> [<code>]"
// line. Returns empty strings when the log isn't accessible or no failure
// has been logged for this gateway since the file last rotated.
func (p *GatewayProvisioner) lastRegFailure(ctx context.Context, gatewayName string) (code, msg string) {
	if p.logDir == "" {
		return "", ""
	}
	path := filepath.Join(p.logDir, "freeswitch.log")
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()

	// Tail the last ~64KB — failures are recent.
	const tailBytes = 64 * 1024
	info, _ := f.Stat()
	size := info.Size()
	off := int64(0)
	if size > tailBytes {
		off = size - tailBytes
	}
	if _, err := f.Seek(off, 0); err != nil {
		return "", ""
	}
	buf := make([]byte, size-off)
	n, _ := f.Read(buf)
	tail := string(buf[:n])

	// Match the FS-formatted line:
	//   <gw> Failed Registration with status Incorrect Authentication [403]. failure #N
	needle := gatewayName + " Failed Registration with status "
	last := strings.LastIndex(tail, needle)
	if last < 0 {
		return "", ""
	}
	rest := tail[last+len(needle):]
	end := strings.IndexAny(rest, "\n.")
	if end < 0 {
		end = len(rest)
	}
	line := rest[:end]
	// Split "Incorrect Authentication [403]" → msg=Incorrect Authentication, code=403
	bo := strings.LastIndex(line, "[")
	bc := strings.LastIndex(line, "]")
	if bo < 0 || bc <= bo+1 {
		return "", strings.TrimSpace(line)
	}
	msg = strings.TrimSpace(line[:bo])
	code = strings.TrimSpace(line[bo+1 : bc])
	return code, msg
}

func splitStatusLine(line string) (string, string, bool) {
	// Sofia uses tabs between key and value but sometimes mixes spaces.
	// Split on the first whitespace run.
	idx := -1
	for i, r := range line {
		if r == ' ' || r == '\t' {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return "", "", false
	}
	k := line[:idx]
	v := strings.TrimSpace(line[idx:])
	if v == "" {
		return "", "", false
	}
	return k, v, true
}

// safeFileBase scrubs anything that isn't ascii-alnum/underscore/dash so a
// malformed gateway name can't escape the dir.
func safeFileBase(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ---------------------------------------------------------------------------
// XML template
// ---------------------------------------------------------------------------

var gatewayTmpl = template.Must(template.New("gw").Parse(`<include>
  <!--
    Auto-generated by sip-platform control-plane. Do not edit.
    Tenant: {{.TenantID}}   Carrier: {{.CarrierKind}}   Account: {{.Name}}
  -->
  {{- $proxyHost := .CarrierProxyHost -}}
  {{- if .ProxyHostOverride -}}{{ $proxyHost = .ProxyHostOverride }}{{- end -}}
  {{- $transport := .CarrierTransport -}}
  {{- if .TransportOverride -}}{{ $transport = .TransportOverride }}{{- end -}}
  {{- if not $transport -}}{{ $transport = "udp" }}{{- end -}}
  {{- /* Realm resolution order: per-trunk override > carrier default > proxy host.
         CallCentric is a notable case where proxy=sip.callcentric.net but
         realm=callcentric.com — they're different values and the realm is
         baked into the digest hash. */ -}}
  {{- $realm := .AuthRealm -}}
  {{- if not $realm -}}{{ $realm = .CarrierDefaultAuthRealm }}{{- end -}}
  {{- if not $realm -}}{{ $realm = $proxyHost }}{{- end -}}
  {{- $proxy := $proxyHost -}}
  {{- if .ProxyPortOverride -}}{{ $proxy = printf "%s:%d" $proxyHost .ProxyPortOverride }}{{- end -}}
  <gateway name="{{.FSGatewayName}}">
    <param name="username" value="{{.SIPUsername}}"/>
    <param name="password" value="{{.SIPPassword}}"/>
    {{- if $realm}}
    <param name="realm" value="{{$realm}}"/>
    <param name="from-domain" value="{{$realm}}"/>
    {{- end}}
    {{- if $proxy}}
    <param name="proxy" value="{{$proxy}}"/>
    {{- end}}
    <param name="register" value="{{if .Register}}true{{else}}false{{end}}"/>
    <param name="register-transport" value="{{$transport}}"/>
    <param name="ping" value="30"/>
    <param name="retry-seconds" value="30"/>
    <param name="expire-seconds" value="600"/>
    <param name="caller-id-in-from" value="false"/>
  </gateway>
</include>
`))

func renderGatewayXML(a store.CarrierAccount) (string, error) {
	var b strings.Builder
	if err := gatewayTmpl.Execute(&b, a); err != nil {
		return "", err
	}
	return b.String(), nil
}
