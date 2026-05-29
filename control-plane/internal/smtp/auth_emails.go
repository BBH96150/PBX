package smtp

import (
	"fmt"
	"strings"
)

// SendInvite sends a workspace-invite email. acceptURL is the full URL the
// recipient should click — typically https://<host>/admin/accept-invite/<token>.
//
// If the Mailer is not Configured(), this is a no-op that returns nil so
// callers don't have to special-case dev environments.
func (m Mailer) SendInvite(to, inviterName, tenantName, acceptURL string) error {
	if !m.Configured() {
		return nil
	}
	subject := fmt.Sprintf("You've been invited to %s on SIP Platform", tenantName)
	body := strings.TrimSpace(fmt.Sprintf(`
Hello,

%s invited you to join %s on SIP Platform.

Click the link below to accept and set up your account:

%s

This link expires in 7 days. If you weren't expecting this invitation, ignore this email.

— SIP Platform
`, valueOr(inviterName, "An administrator"), tenantName, acceptURL))
	return m.Send(to, subject, body, nil)
}

// SendEmailVerification sends a one-time link the user must click to prove
// they control the email address. Used on signup and on explicit resend.
func (m Mailer) SendEmailVerification(to, verifyURL string) error {
	if !m.Configured() {
		return nil
	}
	subject := "Verify your email address"
	body := strings.TrimSpace(fmt.Sprintf(`
Hello,

Please verify your email address for SIP Platform by clicking the link below:

%s

This link expires in 24 hours. If you didn't sign up, ignore this email — no further action needed.

— SIP Platform
`, verifyURL))
	return m.Send(to, subject, body, nil)
}

// SendPasswordReset sends a "click here to reset" email.
func (m Mailer) SendPasswordReset(to, resetURL string) error {
	if !m.Configured() {
		return nil
	}
	subject := "Reset your SIP Platform password"
	body := strings.TrimSpace(fmt.Sprintf(`
Hello,

A password reset was requested for your SIP Platform account.

Click the link below to set a new password:

%s

This link expires in 2 hours. If you didn't request a reset, ignore this email — your password is unchanged.

— SIP Platform
`, resetURL))
	return m.Send(to, subject, body, nil)
}

func valueOr(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
