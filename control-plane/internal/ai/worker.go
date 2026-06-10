package ai

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// InsightStore is the subset of *store.Store the worker needs. Declared as an
// interface so the worker is unit/integration-testable with a fake store and a
// mock Service (no DB, no providers).
type InsightStore interface {
	ListTenantIDsWithPendingInsight(ctx context.Context, limit int) ([]uuid.UUID, error)
	ListCDRsPendingInsight(ctx context.Context, tenantID uuid.UUID, limit int) ([]store.PendingInsightCDR, error)
	CreateCallInsight(ctx context.Context, tenantID *uuid.UUID, callUUID, transcript, summary string, actionItems []string) error
	ListVoicemailMessagesPendingTranscript(ctx context.Context, limit int) ([]store.PendingTranscriptMessage, error)
	SetVoicemailTranscript(ctx context.Context, msgID uuid.UUID, transcript string) error
}

// WebhookFirer is the webhook.Dispatcher capability the worker uses. nil-safe:
// the dispatcher's own Fire is nil-safe, and we also guard the field.
type WebhookFirer interface {
	Fire(tenantID uuid.UUID, event string, payload map[string]any)
}

// Worker is the background insights pipeline. It transcribes call recordings +
// voicemails and (for calls) generates AI summaries, then fires webhooks. It is
// INERT when svc.Enabled() is false.
type Worker struct {
	svc          *Service
	store        InsightStore
	webhooks     WebhookFirer
	recordingDir string // path-traversal root for call recordings
	vmDir        string // path-traversal root for voicemail audio
	perTick      int    // cap on items processed per tick per category
}

// NewWorker constructs the pipeline worker. recordingRoot/vmRoot are the same
// storage roots the portal streaming handlers use; audio paths are validated to
// resolve under them before any file is opened.
func NewWorker(svc *Service, st InsightStore, webhooks WebhookFirer, recordingRoot, vmRoot string) *Worker {
	return &Worker{
		svc:          svc,
		store:        st,
		webhooks:     webhooks,
		recordingDir: recordingRoot,
		vmDir:        vmRoot,
		perTick:      25,
	}
}

// Run is the goroutine entrypoint. If the service is disabled it logs once and
// returns immediately — no ticker, no DB load, no behavior change. Otherwise it
// processes pending recordings + voicemails on each tick until ctx is done.
func (w *Worker) Run(ctx context.Context, interval time.Duration) {
	w.svc.LogStatus()
	if !w.svc.Enabled() {
		return
	}
	if interval <= 0 {
		interval = 60 * time.Second
	}
	t := time.NewTimer(15 * time.Second) // small initial delay so boot settles
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
			t.Reset(interval)
		}
	}
}

// tick processes one batch of recordings and one batch of voicemails. Per-item
// errors are logged and tolerated so one bad file never stalls the queue.
func (w *Worker) tick(ctx context.Context) {
	w.processRecordings(ctx)
	w.processVoicemails(ctx)
}

func (w *Worker) processRecordings(ctx context.Context) {
	tenantIDs, err := w.store.ListTenantIDsWithPendingInsight(ctx, 100)
	if err != nil {
		slog.Error("ai worker: list pending tenants", "err", err)
		return
	}
	for _, tid := range tenantIDs {
		select {
		case <-ctx.Done():
			return
		default:
		}
		cdrs, err := w.store.ListCDRsPendingInsight(ctx, tid, w.perTick)
		if err != nil {
			slog.Error("ai worker: list pending CDRs", "tenant", tid, "err", err)
			continue
		}
		for _, c := range cdrs {
			w.processOneRecording(ctx, c)
		}
	}
}

func (w *Worker) processOneRecording(ctx context.Context, c store.PendingInsightCDR) {
	path, ok := safePathUnder(w.recordingDir, c.RecordingPath)
	if !ok {
		slog.Warn("ai worker: recording path outside root, skipping", "call_uuid", c.CallUUID)
		return
	}
	transcript, err := w.svc.Transcribe(ctx, path)
	if err != nil {
		slog.Error("ai worker: transcribe recording", "call_uuid", c.CallUUID, "err", err)
		return
	}
	var insight Insight
	if transcript != "" {
		insight, err = w.svc.Summarize(ctx, transcript)
		if err != nil {
			// Keep the transcript even if summarization fails.
			slog.Error("ai worker: summarize", "call_uuid", c.CallUUID, "err", err)
		}
	}
	if err := w.store.CreateCallInsight(ctx, c.TenantID, c.CallUUID, transcript, insight.Summary, insight.ActionItems); err != nil {
		slog.Error("ai worker: store insight", "call_uuid", c.CallUUID, "err", err)
		return
	}
	if w.webhooks != nil && c.TenantID != nil {
		w.webhooks.Fire(*c.TenantID, "call.summarized", map[string]any{
			"call_uuid":      c.CallUUID,
			"summary":        insight.Summary,
			"action_items":   insight.ActionItems,
			"has_transcript": transcript != "",
		})
	}
}

func (w *Worker) processVoicemails(ctx context.Context) {
	msgs, err := w.store.ListVoicemailMessagesPendingTranscript(ctx, w.perTick)
	if err != nil {
		slog.Error("ai worker: list pending voicemails", "err", err)
		return
	}
	for _, m := range msgs {
		select {
		case <-ctx.Done():
			return
		default:
		}
		w.processOneVoicemail(ctx, m)
	}
}

func (w *Worker) processOneVoicemail(ctx context.Context, m store.PendingTranscriptMessage) {
	path, ok := safePathUnder(w.vmDir, m.AudioPath)
	if !ok {
		slog.Warn("ai worker: voicemail path outside root, skipping", "msg", m.ID)
		return
	}
	transcript, err := w.svc.Transcribe(ctx, path)
	if err != nil {
		slog.Error("ai worker: transcribe voicemail", "msg", m.ID, "err", err)
		return
	}
	if transcript == "" {
		return // nothing to store; retried next tick
	}
	if err := w.store.SetVoicemailTranscript(ctx, m.ID, transcript); err != nil {
		slog.Error("ai worker: store voicemail transcript", "msg", m.ID, "err", err)
		return
	}
	if w.webhooks != nil {
		w.webhooks.Fire(m.TenantID, "voicemail.transcribed", map[string]any{
			"message_id": m.ID.String(),
			"transcript": transcript,
		})
	}
}

// safePathUnder cleans p and confirms it sits under root (path-traversal guard,
// same logic as the portal recording/voicemail streaming handlers). Returns
// false when root is empty or p escapes it.
func safePathUnder(root, p string) (string, bool) {
	if root == "" || p == "" {
		return "", false
	}
	root = filepath.Clean(root)
	clean := filepath.Clean(p)
	if clean != root && !strings.HasPrefix(clean, root+string(os.PathSeparator)) {
		return "", false
	}
	return clean, true
}
