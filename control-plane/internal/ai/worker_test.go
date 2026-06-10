package ai

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/tendpos/sip-platform/control-plane/internal/store"
)

// fakeStore implements InsightStore in-memory for worker tests.
type fakeStore struct {
	tenants        []uuid.UUID
	cdrs           map[uuid.UUID][]store.PendingInsightCDR
	voicemails     []store.PendingTranscriptMessage
	createdInsight []createdInsight
	setTranscripts map[uuid.UUID]string
}

type createdInsight struct {
	callUUID    string
	transcript  string
	summary     string
	actionItems []string
}

func (f *fakeStore) ListTenantIDsWithPendingInsight(_ context.Context, _ int) ([]uuid.UUID, error) {
	return f.tenants, nil
}
func (f *fakeStore) ListCDRsPendingInsight(_ context.Context, tid uuid.UUID, _ int) ([]store.PendingInsightCDR, error) {
	return f.cdrs[tid], nil
}
func (f *fakeStore) CreateCallInsight(_ context.Context, _ *uuid.UUID, callUUID, transcript, summary string, items []string) error {
	f.createdInsight = append(f.createdInsight, createdInsight{callUUID, transcript, summary, items})
	return nil
}
func (f *fakeStore) ListVoicemailMessagesPendingTranscript(_ context.Context, _ int) ([]store.PendingTranscriptMessage, error) {
	return f.voicemails, nil
}
func (f *fakeStore) SetVoicemailTranscript(_ context.Context, msgID uuid.UUID, transcript string) error {
	if f.setTranscripts == nil {
		f.setTranscripts = map[uuid.UUID]string{}
	}
	f.setTranscripts[msgID] = transcript
	return nil
}

// fakeWebhooks records fired events.
type fakeWebhooks struct {
	fired []string
}

func (f *fakeWebhooks) Fire(_ uuid.UUID, event string, _ map[string]any) {
	f.fired = append(f.fired, event)
}

func TestWorkerDisabledIsInert(t *testing.T) {
	fs := &fakeStore{tenants: []uuid.UUID{uuid.New()}}
	w := NewWorker(New(Config{}), fs, &fakeWebhooks{}, "/rec", "/vm")
	// tick must be safe even disabled, and Run returns immediately.
	w.Run(context.Background(), 0)
	if len(fs.createdInsight) != 0 {
		t.Fatal("disabled worker must not create insights")
	}
}

func TestWorkerProcessesRecordingsAndVoicemails(t *testing.T) {
	dir := t.TempDir()
	recPath := filepath.Join(dir, "rec.wav")
	vmPath := filepath.Join(dir, "vm.wav")
	if err := os.WriteFile(recPath, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vmPath, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}

	tid := uuid.New()
	msgID := uuid.New()
	fs := &fakeStore{
		tenants: []uuid.UUID{tid},
		cdrs: map[uuid.UUID][]store.PendingInsightCDR{
			tid: {{ID: uuid.New(), TenantID: &tid, CallUUID: "call-1", RecordingPath: recPath}},
		},
		voicemails: []store.PendingTranscriptMessage{
			{ID: msgID, TenantID: tid, AudioPath: vmPath},
		},
	}
	wh := &fakeWebhooks{}
	svc := NewWithImpls(
		&mockTranscriber{out: "hello world"},
		&mockSummarizer{out: Insight{Summary: "greeting", ActionItems: []string{"reply"}}},
	)
	w := NewWorker(svc, fs, wh, dir, dir)

	w.tick(context.Background())

	if len(fs.createdInsight) != 1 {
		t.Fatalf("want 1 insight, got %d", len(fs.createdInsight))
	}
	ci := fs.createdInsight[0]
	if ci.callUUID != "call-1" || ci.transcript != "hello world" || ci.summary != "greeting" {
		t.Fatalf("unexpected insight: %+v", ci)
	}
	if got := fs.setTranscripts[msgID]; got != "hello world" {
		t.Fatalf("voicemail transcript = %q, want %q", got, "hello world")
	}
	// Both webhooks fired.
	var sawCall, sawVM bool
	for _, e := range wh.fired {
		switch e {
		case "call.summarized":
			sawCall = true
		case "voicemail.transcribed":
			sawVM = true
		}
	}
	if !sawCall || !sawVM {
		t.Fatalf("expected both webhooks, got %v", wh.fired)
	}
}

func TestWorkerPathTraversalGuard(t *testing.T) {
	tid := uuid.New()
	fs := &fakeStore{
		tenants: []uuid.UUID{tid},
		cdrs: map[uuid.UUID][]store.PendingInsightCDR{
			// Path escapes the recording root → skipped, no insight.
			tid: {{ID: uuid.New(), TenantID: &tid, CallUUID: "evil", RecordingPath: "/etc/passwd"}},
		},
	}
	svc := NewWithImpls(&mockTranscriber{out: "x"}, &mockSummarizer{})
	w := NewWorker(svc, fs, &fakeWebhooks{}, "/var/recordings", "/var/vm")
	w.tick(context.Background())
	if len(fs.createdInsight) != 0 {
		t.Fatal("path-traversal recording should be skipped")
	}
}

func TestWorkerEmptyTranscriptSkipsVoicemailStore(t *testing.T) {
	dir := t.TempDir()
	vmPath := filepath.Join(dir, "vm.wav")
	_ = os.WriteFile(vmPath, []byte("a"), 0o600)
	msgID := uuid.New()
	fs := &fakeStore{
		voicemails: []store.PendingTranscriptMessage{{ID: msgID, TenantID: uuid.New(), AudioPath: vmPath}},
	}
	// Transcriber returns "" (no speech) → nothing stored.
	w := NewWorker(NewWithImpls(&mockTranscriber{out: ""}, nil), fs, &fakeWebhooks{}, dir, dir)
	w.processVoicemails(context.Background())
	if _, ok := fs.setTranscripts[msgID]; ok {
		t.Fatal("empty transcript should not be stored")
	}
}
