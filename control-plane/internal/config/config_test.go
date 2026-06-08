package config

import "testing"

func TestEnvOr(t *testing.T) {
	t.Setenv("CFG_TEST_KEY", "")
	if got := envOr("CFG_TEST_KEY", "fallback"); got != "fallback" {
		t.Errorf("empty → default: got %q", got)
	}
	t.Setenv("CFG_TEST_KEY", "set")
	if got := envOr("CFG_TEST_KEY", "fallback"); got != "set" {
		t.Errorf("set → value: got %q", got)
	}
}

func TestFromEnvRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected error when DATABASE_URL is unset")
	}
}

func TestFromEnvDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@db:5432/x")
	// Force defaults by clearing overrides.
	for _, k := range []string{"CONTROL_PLANE_ADMIN_ADDR", "ESL_HOST", "ESL_PORT", "SIP_PUBLIC_PORT", "REDIS_URL", "PORTAL_BASE_URL"} {
		t.Setenv(k, "")
	}
	c, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if c.AdminAddr != ":8080" {
		t.Errorf("AdminAddr default: %q", c.AdminAddr)
	}
	if c.ESLHost != "freeswitch" {
		t.Errorf("ESLHost default: %q", c.ESLHost)
	}
	if c.ESLPort != 8021 {
		t.Errorf("ESLPort default: %d", c.ESLPort)
	}
	if c.SIPPublicPort != 5060 {
		t.Errorf("SIPPublicPort default: %d", c.SIPPublicPort)
	}
	if c.DatabaseURL != "postgres://u:p@db:5432/x" {
		t.Errorf("DatabaseURL not carried: %q", c.DatabaseURL)
	}
}

func TestFromEnvInvalidESLPort(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@db:5432/x")
	t.Setenv("ESL_PORT", "not-a-number")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected error for non-numeric ESL_PORT")
	}
}

func TestFromEnvSIPDomainSuffixTrimsLeadingDot(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@db:5432/x")
	t.Setenv("SIP_DOMAIN_SUFFIX", ".pbx.example.com")
	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.SIPDomainSuffix != "pbx.example.com" {
		t.Errorf("leading dot not trimmed: %q", c.SIPDomainSuffix)
	}
}

func TestFromEnvSMTPStartTLS(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@db:5432/x")
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_PORT", "2525")
	t.Setenv("SMTP_STARTTLS", "TRUE")
	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !c.SMTPUseSTARTTLS {
		t.Error("SMTP_STARTTLS=TRUE should parse true (case-insensitive)")
	}
	if c.SMTPPort != 2525 {
		t.Errorf("SMTPPort: %d", c.SMTPPort)
	}
}
