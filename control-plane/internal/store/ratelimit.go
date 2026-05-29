package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Rate-limit thresholds — kept liberal enough not to block normal users
// who fat-finger their password a few times, strict enough to defeat
// password spraying. Per-email is the primary defense; per-IP catches
// distributed-credential-stuffing within a single source.
const (
	loginRateWindow         = 15 * time.Minute
	loginRateMaxPerEmail    = 10
	loginRateMaxPerIPGlobal = 30 // across all emails from one IP
)

// LoginRateLimited returns (limited, reason) after consulting Redis counters
// for both the email and the IP. Caller should reject login attempts with
// a friendly message + audit.login.rate_limited event.
func (s *Store) LoginRateLimited(ctx context.Context, email, ip string) (bool, string) {
	if s.Redis == nil {
		return false, ""
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email != "" {
		n, _ := s.Redis.Get(ctx, loginRateKeyEmail(email)).Int()
		if n >= loginRateMaxPerEmail {
			return true, "Too many failed sign-in attempts for this account. Try again in 15 minutes."
		}
	}
	if ip != "" {
		n, _ := s.Redis.Get(ctx, loginRateKeyIP(ip)).Int()
		if n >= loginRateMaxPerIPGlobal {
			return true, "Too many failed sign-in attempts from this network. Try again in 15 minutes."
		}
	}
	return false, ""
}

// RecordLoginFailure bumps both counters. Called whenever a password verify
// returns an error, AND whenever a 2FA challenge fails for an established
// session (catches the "stolen-password but no 2FA code" attacker).
func (s *Store) RecordLoginFailure(ctx context.Context, email, ip string) {
	if s.Redis == nil {
		return
	}
	email = strings.ToLower(strings.TrimSpace(email))
	pipe := s.Redis.Pipeline()
	if email != "" {
		k := loginRateKeyEmail(email)
		pipe.Incr(ctx, k)
		pipe.Expire(ctx, k, loginRateWindow)
	}
	if ip != "" {
		k := loginRateKeyIP(ip)
		pipe.Incr(ctx, k)
		pipe.Expire(ctx, k, loginRateWindow)
	}
	_, _ = pipe.Exec(ctx)
}

// ResetLoginCounters clears the per-email counter after a successful login.
// We intentionally do NOT clear the per-IP counter — a real user logging in
// successfully doesn't prove the rest of the spray traffic from that IP is
// legitimate.
func (s *Store) ResetLoginCounters(ctx context.Context, email string) {
	if s.Redis == nil {
		return
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email != "" {
		_ = s.Redis.Del(ctx, loginRateKeyEmail(email)).Err()
	}
}

func loginRateKeyEmail(email string) string { return fmt.Sprintf("login:fail:email:%s", email) }
func loginRateKeyIP(ip string) string       { return fmt.Sprintf("login:fail:ip:%s", ip) }
