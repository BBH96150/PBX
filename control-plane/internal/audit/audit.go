// Package audit is a thin write-side wrapper around the audit_log table.
//
// Design: every security-relevant action funnels through Log(). Failure to
// write an audit row never blocks the user-facing action — we log a warning
// and continue. Callers pass an Event with as much context as they can; the
// helper fills in IP + UA from the request when present.
package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Event struct {
	TenantID     *uuid.UUID
	ActorUserID  *uuid.UUID
	ActorTokenID *uuid.UUID
	ActorEmail   string // snapshot for failed-login / post-deletion lookback
	Event        string // e.g. "auth.login.success"
	TargetType   string // "user","tenant","invite","api_token", ...
	TargetID     *uuid.UUID
	IPAddress    string // empty → NULL
	UserAgent    string
	Payload      map[string]any
}

type Logger struct {
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Logger { return &Logger{db: db} }

// Log inserts the row. Errors are warned, never returned, so callers can
// `audit.Log(...)` without poisoning their happy path.
func (l *Logger) Log(ctx context.Context, e Event) {
	if l == nil || l.db == nil {
		return
	}
	if e.Event == "" {
		return
	}
	payload := e.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		body = []byte("{}")
	}
	var ip any
	if e.IPAddress != "" {
		ip = e.IPAddress
	}
	_, err = l.db.Exec(ctx, `
		INSERT INTO audit_log
		    (tenant_id, actor_user_id, actor_token_id, actor_email,
		     event, target_type, target_id, ip_address, user_agent, payload)
		VALUES ($1,$2,$3,NULLIF($4,''),$5,NULLIF($6,''),$7,$8,NULLIF($9,''),$10::jsonb)`,
		e.TenantID, e.ActorUserID, e.ActorTokenID, e.ActorEmail,
		e.Event, e.TargetType, e.TargetID, ip, e.UserAgent, string(body),
	)
	if err != nil {
		slog.Warn("audit insert failed", "event", e.Event, "err", err)
	}
}

// FromRequest pulls the client IP + UA off an http.Request. Honors X-Forwarded-For
// since the control plane sits behind a reverse proxy in real deploys.
func FromRequest(r *http.Request) (ip, ua string) {
	if r == nil {
		return "", ""
	}
	ua = r.UserAgent()
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Left-most is the original client.
		if i := strings.IndexByte(xff, ','); i > 0 {
			ip = strings.TrimSpace(xff[:i])
		} else {
			ip = strings.TrimSpace(xff)
		}
		return ip, ua
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr, ua
	}
	return host, ua
}

// Event-name constants — keep these stable; ops queries pin to them.
const (
	EventLoginSuccess         = "auth.login.success"
	EventLoginFailure         = "auth.login.failure"
	EventLoginBlockedUnverified = "auth.login.blocked_unverified"
	EventLogout               = "auth.logout"
	EventSignup               = "auth.signup"
	EventPasswordResetRequest = "auth.password_reset.request"
	EventPasswordResetConsume = "auth.password_reset.complete"
	EventInviteCreate         = "invite.create"
	EventInviteAccept         = "invite.accept"
	EventInviteRevoke         = "invite.revoke"
	EventAPITokenCreate       = "api_token.create"
	EventAPITokenRevoke       = "api_token.revoke"
	EventTenantCreate         = "tenant.create"
	EventEmailVerified        = "auth.email.verified"
)
