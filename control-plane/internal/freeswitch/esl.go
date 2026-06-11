package freeswitch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/percipia/eslgo"
	"github.com/percipia/eslgo/command"

	"github.com/tendpos/sip-platform/control-plane/internal/smtp"
	"github.com/tendpos/sip-platform/control-plane/internal/store"
	"github.com/tendpos/sip-platform/control-plane/internal/webhook"
)

// ESLClient connects to FreeSWITCH's Event Socket and:
//
//   - dispatches inbound events:
//       CHANNEL_HANGUP_COMPLETE (A-leg only) → CDR writer
//       CUSTOM voicemail::maintenance (leave-message) → VM persist + email
//   - exposes outbound provisioning calls (Wave 4.5):
//       callcenter_config commands for queue / agent / tier changes
//
// The live *eslgo.Conn is held in an atomic.Pointer so admin API handlers
// can SendCommand from any goroutine without coordinating with the receive
// loop. When the connection drops, the pointer is set to nil and any
// provisioning call returns ErrNotConnected — callers log and continue
// (the admin can fall back to `reload mod_callcenter`).
type ESLClient struct {
	host           string
	port           int
	password       string
	store          *store.Store
	mailer         smtp.Mailer
	webhooks       *webhook.Dispatcher
	kamailioTarget string // for agent contact URI rendering during sync
	portalBaseURL  string // for the "listen in portal" link in VM-to-email

	conn atomic.Pointer[eslgo.Conn]

	// apiMu serializes synchronous API calls so concurrent callers don't
	// cross-correlate responses. eslgo uses a single typed channel per
	// response type, which means two goroutines calling SendCommand at the
	// same time can end up with each other's responses. Holding this mutex
	// for the full request-response round-trip avoids that.
	//
	// The cost is API throughput — at most one synchronous API call in
	// flight at a time. That's fine for our usage (status polling at 2s,
	// occasional manual originate). bgapi-style calls are unaffected
	// because their response comes back on a separate channel keyed by
	// Job-UUID and don't share state with sync API.
	apiMu sync.Mutex
}

// ErrNotConnected is returned by provisioning methods when no ESL session
// is established.
var ErrNotConnected = errors.New("ESL not connected")

func NewESLClient(host string, port int, password string, st *store.Store, mailer smtp.Mailer, wh *webhook.Dispatcher, kamailioTarget string) *ESLClient {
	return &ESLClient{
		host: host, port: port, password: password,
		store: st, mailer: mailer, webhooks: wh, kamailioTarget: kamailioTarget,
	}
}

// SetPortalBaseURL configures the base URL used to build the "listen in portal"
// link in voicemail-to-email notifications (e.g. https://app.example.com). When
// empty the email omits a clickable link path and just references the inbox.
func (c *ESLClient) SetPortalBaseURL(u string) { c.portalBaseURL = u }

// Run blocks until ctx is canceled. Reconnects with exponential backoff
// (capped at 30s) on every disconnect or auth failure.
func (c *ESLClient) Run(ctx context.Context) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		err := c.runOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			slog.Warn("esl connection lost", "err", err, "retry_in", backoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		} else {
			backoff = 30 * time.Second
		}
	}
}

func (c *ESLClient) runOnce(ctx context.Context) error {
	addr := net.JoinHostPort(c.host, strconv.Itoa(c.port))

	disconnected := make(chan struct{})
	conn, err := eslgo.Dial(addr, c.password, func() {
		close(disconnected)
	})
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer conn.ExitAndClose()
	slog.Info("esl connected", "addr", addr)
	c.conn.Store(conn)
	defer c.conn.Store(nil)

	subCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if err := conn.EnableEvents(subCtx, "plain"); err != nil {
		cancel()
		return fmt.Errorf("enable events: %w", err)
	}
	cancel()

	listenerID := conn.RegisterEventListener(eslgo.EventListenAll, func(event *eslgo.Event) {
		switch {
		case shouldRecordCDR(event):
			c.handleCDR(event)
		case isVoicemailLeaveMessage(event):
			c.handleVoicemailLeaveMessage(event)
		}
	})
	defer conn.RemoveEventListener(eslgo.EventListenAll, listenerID)

	select {
	case <-ctx.Done():
		return nil
	case <-disconnected:
		return fmt.Errorf("esl peer disconnected")
	}
}

// callAPI runs one FS API command via bgapi. Splits the input string on
// the first space ("callcenter_config queue load foo" → Command="callcenter_config",
// Arguments="queue load foo").
func (c *ESLClient) callAPI(ctx context.Context, cmd string) error {
	conn := c.conn.Load()
	if conn == nil {
		return ErrNotConnected
	}
	head, args, _ := strings.Cut(cmd, " ")
	_, err := conn.SendCommand(ctx, command.API{
		Command:    head,
		Arguments:  args,
		Background: true,
	})
	return err
}

// Originate kicks off an outbound call via FreeSWITCH's `bgapi originate`.
//
// Synchronous originate would block the ESL socket for the entire call —
// dial, ring (up to 30s+), answer, application execution. While blocked, no
// other ESL API call can proceed (the mutex around CallAPISync forces
// serial access), and concurrent status polls pile up behind it until TCP
// write buffers exhaust and we see "i/o timeout" errors.
//
// bgapi returns immediately with "+OK Job-UUID: <uuid>". The actual call
// result fires later as a BACKGROUND_JOB event. For a UI test-call button,
// "we accepted your dial" is the right signal — the user's phone either
// rings or it doesn't, and we can surface failures via the gateway's
// CallsOUT counters and the FS log if needed.
//
// dialString is the full FS originate argument up to the application:
//   "{key=value,key2=v2}sofia/gateway/foo/12345"
// application is what runs when the leg answers, e.g. "&echo" or
// "&playback(misc/welcome.wav)".
//
// Returns the raw "+OK Job-UUID: <uuid>" or "-ERR <reason>" string so
// callers can do their own classification.
func (c *ESLClient) Originate(ctx context.Context, dialString, application string) (string, error) {
	conn := c.conn.Load()
	if conn == nil {
		return "", ErrNotConnected
	}
	// Hold the mutex only for the send + immediate "+OK Job-UUID" reply,
	// not for the call duration.
	c.apiMu.Lock()
	defer c.apiMu.Unlock()
	resp, err := conn.SendCommand(ctx, command.API{
		Command:    "originate",
		Arguments:  dialString + " " + application,
		Background: true,
	})
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", nil
	}
	// bgapi puts the result in the Reply header (Headers["Reply-Text"]),
	// not the body. Body is empty.
	if r := resp.Headers.Get("Reply-Text"); r != "" {
		return strings.TrimSpace(r), nil
	}
	return string(resp.Body), nil
}

// CallAPISync runs one FS API command synchronously and returns the response
// body. Used by status endpoints that need to render real-time info to the
// portal (gateway state, registered contacts, active calls).
//
// Holds c.apiMu so concurrent callers don't end up with each other's
// responses — see field doc on ESLClient.
func (c *ESLClient) CallAPISync(ctx context.Context, cmd string) (string, error) {
	conn := c.conn.Load()
	if conn == nil {
		return "", ErrNotConnected
	}
	c.apiMu.Lock()
	defer c.apiMu.Unlock()
	head, args, _ := strings.Cut(cmd, " ")
	resp, err := conn.SendCommand(ctx, command.API{
		Command:    head,
		Arguments:  args,
		Background: false,
	})
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", nil
	}
	return string(resp.Body), nil
}

// handleCDR persists one CDR for an A-leg CHANNEL_HANGUP_COMPLETE.
func (c *ESLClient) handleCDR(event *eslgo.Event) {
	cdr := eventToCDR(event)
	wctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.store.CreateCDR(wctx, cdr); err != nil {
		slog.Error("cdr write failed", "uuid", cdr.CallUUID, "err", err)
		return
	}
	slog.Info("cdr persisted",
		"uuid", cdr.CallUUID,
		"direction", cdr.Direction,
		"from", cdr.FromURI,
		"to", cdr.ToURI,
		"duration", cdr.DurationSec,
		"disposition", cdr.Disposition,
		"hangup_cause", cdr.HangupCause,
	)
	if cdr.TenantID != nil {
		c.webhooks.Fire(*cdr.TenantID, "call.completed", map[string]any{
			"call_uuid":      cdr.CallUUID,
			"direction":      cdr.Direction,
			"from":           cdr.FromURI,
			"to":             cdr.ToURI,
			"caller_id_num":  cdr.CallerIDNum,
			"caller_id_name": cdr.CallerIDName,
			"started_at":     cdr.StartedAt.UTC().Format(time.RFC3339),
			"duration_sec":   cdr.DurationSec,
			"billable_sec":   cdr.BillableSec,
			"disposition":    cdr.Disposition,
			"hangup_cause":   cdr.HangupCause,
		})
	}
}
