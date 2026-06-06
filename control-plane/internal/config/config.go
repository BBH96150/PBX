package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	AdminAddr         string
	ProvisioningAddr  string
	ProvisioningCert  string
	ProvisioningKey   string
	ProvisioningHost  string
	DatabaseURL       string
	RedisURL          string
	ESLHost           string
	ESLPort           int
	ESLPassword       string
	LogLevel          string
	KamailioSIPTarget string // host:port; used in FS bridge dialplan

	// SIP public address handed to provisioned phones as the outbound proxy.
	SIPPublicHost      string
	SIPPublicPort      int
	SIPPublicTransport string // udp, tcp, tls

	// SMTP relay for voicemail-to-email (Wave 2.5). Leave SMTPHost empty
	// to skip email delivery (messages still land on disk + in voicemail_messages).
	SMTPHost        string
	SMTPPort        int
	SMTPUsername    string
	SMTPPassword    string
	SMTPFrom        string
	SMTPUseSTARTTLS bool

	// AlertEmail receives operational alerts (e.g. trunk registration drops).
	// Empty disables alert emails (events are still logged at WARN).
	AlertEmail string

	// Manufacturer RPS / true ZTP (Task #10). Each vendor has its own
	// account/portal. Leave empty to fall back to LogOnly (the integration
	// records the attempt without calling the vendor).
	PolycomZTPAPIBase   string
	PolycomZTPAPIToken  string
	PolycomZTPProfileID string

	YealinkRPSAPIBase    string
	YealinkRPSAPIToken   string
	YealinkRPSServerName string

	GrandstreamGDMSAPIBase  string
	GrandstreamGDMSAPIToken string
	GrandstreamGDMSOrgID    string

	// Phase 4.0: bootstrap super-admin API token. Inserted on startup only
	// when api_tokens is empty. Token format: sip_<48hex>. Leave empty in
	// envs where another admin has already provisioned tokens.
	BootstrapAPIToken string

	// Phase 4.3: bootstrap super-admin portal user. Inserted on startup
	// only when the `users` table is empty.
	BootstrapUserEmail    string
	BootstrapUserPassword string

	// Phase 4.5: portal URL used in emailed invite + reset links.
	PortalBaseURL string

	// Phase 5.1: shared dir where the control-plane writes per-tenant
	// carrier gateway XML for FreeSWITCH to pick up via sofia rescan.
	// Must be the same path mounted into both containers in compose.
	FSDynamicGatewayDir string

	// Phase 5.1: read-only mount of FS's log dir so we can surface
	// registration failures (e.g., "403 Incorrect Authentication") in
	// the trunks page status fragment.
	FSLogDir string

	// Voicemail inbox: read-only mount of FS's storage dir so the control
	// plane can stream recordings to the portal. Recorded audio_path values
	// must resolve under this root (path-traversal guard).
	VoicemailStorageRoot string

	// Call recording playback: read-only mount of FS's recordings dir.
	// CDR recording_path values must resolve under this root.
	RecordingStorageRoot string

	// Phase A.1 (Wildcard SIP domains): suffix appended to the tenant
	// slug to auto-generate the primary sip_domain on tenant create.
	// E.g., suffix "pbx.tendpos.com" + slug "bbh" → "bbh.pbx.tendpos.com".
	// When empty (dev/test), tenant create still works but no sip_domain
	// is auto-generated — the operator picks one manually on the tenant
	// detail page. Requires a *.<suffix> wildcard A record pointing at
	// the platform's public IP so Linphone / desk phones resolve it
	// without per-tenant /etc/hosts hacks.
	SIPDomainSuffix string
}

func FromEnv() (*Config, error) {
	c := &Config{
		AdminAddr:          envOr("CONTROL_PLANE_ADMIN_ADDR", ":8080"),
		ProvisioningAddr:   envOr("CONTROL_PLANE_PROVISIONING_ADDR", ":8443"),
		ProvisioningCert:   os.Getenv("PROVISIONING_TLS_CERT"),
		ProvisioningKey:    os.Getenv("PROVISIONING_TLS_KEY"),
		ProvisioningHost:   envOr("PROVISIONING_PUBLIC_HOST", "provision.example.local"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		RedisURL:           envOr("REDIS_URL", "redis://redis:6379/0"),
		ESLHost:            envOr("ESL_HOST", "freeswitch"),
		ESLPassword:        envOr("ESL_PASSWORD", "ClueCon"),
		LogLevel:           envOr("CONTROL_PLANE_LOG_LEVEL", "info"),
		KamailioSIPTarget:  envOr("KAMAILIO_SIP_TARGET", "kamailio:5060"),
		SIPPublicHost:      envOr("SIP_PUBLIC_HOST", "sip.example.local"),
		SIPPublicTransport: envOr("SIP_PUBLIC_TRANSPORT", "udp"),
	}
	port, err := strconv.Atoi(envOr("ESL_PORT", "8021"))
	if err != nil {
		return nil, fmt.Errorf("ESL_PORT: %w", err)
	}
	c.ESLPort = port
	sipPort, err := strconv.Atoi(envOr("SIP_PUBLIC_PORT", "5060"))
	if err != nil {
		return nil, fmt.Errorf("SIP_PUBLIC_PORT: %w", err)
	}
	c.SIPPublicPort = sipPort

	c.PolycomZTPAPIBase = os.Getenv("POLYCOM_ZTP_API_BASE")
	c.PolycomZTPAPIToken = os.Getenv("POLYCOM_ZTP_API_TOKEN")
	c.PolycomZTPProfileID = os.Getenv("POLYCOM_ZTP_PROFILE_ID")

	c.YealinkRPSAPIBase = os.Getenv("YEALINK_RPS_API_BASE")
	c.YealinkRPSAPIToken = os.Getenv("YEALINK_RPS_API_TOKEN")
	c.YealinkRPSServerName = os.Getenv("YEALINK_RPS_SERVER_NAME")

	c.GrandstreamGDMSAPIBase = os.Getenv("GRANDSTREAM_GDMS_API_BASE")
	c.GrandstreamGDMSAPIToken = os.Getenv("GRANDSTREAM_GDMS_API_TOKEN")
	c.GrandstreamGDMSOrgID = os.Getenv("GRANDSTREAM_GDMS_ORG_ID")

	c.AlertEmail = os.Getenv("ALERT_EMAIL")

	c.BootstrapAPIToken = os.Getenv("BOOTSTRAP_API_TOKEN")
	c.BootstrapUserEmail = os.Getenv("BOOTSTRAP_USER_EMAIL")
	c.BootstrapUserPassword = os.Getenv("BOOTSTRAP_USER_PASSWORD")
	c.PortalBaseURL = envOr("PORTAL_BASE_URL", "http://localhost:8080")
	c.FSDynamicGatewayDir = envOr("FS_DYNAMIC_GATEWAY_DIR", "/fs-gateways")
	c.FSLogDir = envOr("FS_LOG_DIR", "/fs-logs")
	c.VoicemailStorageRoot = envOr("VOICEMAIL_STORAGE_ROOT", "/var/lib/freeswitch/storage")
	c.RecordingStorageRoot = envOr("RECORDING_STORAGE_ROOT", "/var/lib/freeswitch/recordings")
	c.SIPDomainSuffix = strings.TrimPrefix(os.Getenv("SIP_DOMAIN_SUFFIX"), ".")

	c.SMTPHost = os.Getenv("SMTP_HOST")
	c.SMTPUsername = os.Getenv("SMTP_USERNAME")
	c.SMTPPassword = os.Getenv("SMTP_PASSWORD")
	c.SMTPFrom = os.Getenv("SMTP_FROM")
	c.SMTPUseSTARTTLS = strings.EqualFold(os.Getenv("SMTP_STARTTLS"), "true")
	if c.SMTPHost != "" {
		smtpPort, err := strconv.Atoi(envOr("SMTP_PORT", "587"))
		if err != nil {
			return nil, fmt.Errorf("SMTP_PORT: %w", err)
		}
		c.SMTPPort = smtpPort
	}

	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	return c, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
