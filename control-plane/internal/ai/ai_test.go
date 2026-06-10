package ai

import (
	"context"
	"errors"
	"testing"
)

// mockTranscriber / mockSummarizer let us assert delegation without touching a
// real provider.
type mockTranscriber struct {
	gotPath string
	out     string
	err     error
}

func (m *mockTranscriber) Transcribe(_ context.Context, audioPath string) (string, error) {
	m.gotPath = audioPath
	return m.out, m.err
}

type mockSummarizer struct {
	gotTranscript string
	out           Insight
	err           error
}

func (m *mockSummarizer) Summarize(_ context.Context, transcript string) (Insight, error) {
	m.gotTranscript = transcript
	return m.out, m.err
}

func TestServiceDisabledByDefault(t *testing.T) {
	// No provider / no keys → disabled, mirroring NewSealerFromEnv(nil).
	s := New(Config{})
	if s.Enabled() {
		t.Fatal("empty config should be disabled")
	}
	if s.SummariesEnabled() {
		t.Fatal("empty config should have summaries disabled")
	}
	// Inert no-ops, no error.
	tr, err := s.Transcribe(context.Background(), "/some/path.wav")
	if err != nil || tr != "" {
		t.Fatalf("disabled Transcribe: got (%q, %v), want (\"\", nil)", tr, err)
	}
	ins, err := s.Summarize(context.Background(), "hello")
	if err != nil || ins.Summary != "" || len(ins.ActionItems) != 0 {
		t.Fatalf("disabled Summarize: got (%+v, %v)", ins, err)
	}
}

func TestServiceEnabledGating(t *testing.T) {
	cases := []struct {
		name           string
		cfg            Config
		wantEnabled    bool
		wantSummEnable bool
	}{
		{"no provider", Config{AnthropicAPIKey: "x"}, false, true},
		{"provider no key", Config{TranscriptionProvider: "deepgram"}, false, false},
		{"deepgram key only", Config{TranscriptionProvider: "deepgram", DeepgramAPIKey: "k"}, true, false},
		{"unknown provider", Config{TranscriptionProvider: "whisper", DeepgramAPIKey: "k"}, false, false},
		{"full", Config{TranscriptionProvider: "deepgram", DeepgramAPIKey: "k", AnthropicAPIKey: "a"}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New(tc.cfg)
			if s.Enabled() != tc.wantEnabled {
				t.Errorf("Enabled()=%v want %v", s.Enabled(), tc.wantEnabled)
			}
			if s.SummariesEnabled() != tc.wantSummEnable {
				t.Errorf("SummariesEnabled()=%v want %v", s.SummariesEnabled(), tc.wantSummEnable)
			}
		})
	}
}

func TestServiceDelegates(t *testing.T) {
	mt := &mockTranscriber{out: "the transcript"}
	ms := &mockSummarizer{out: Insight{Summary: "sum", ActionItems: []string{"call back"}}}
	s := NewWithImpls(mt, ms)

	if !s.Enabled() || !s.SummariesEnabled() {
		t.Fatal("service with both impls should be fully enabled")
	}

	tr, err := s.Transcribe(context.Background(), "/rec/abc.wav")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if tr != "the transcript" || mt.gotPath != "/rec/abc.wav" {
		t.Fatalf("Transcribe delegation: got %q path %q", tr, mt.gotPath)
	}

	ins, err := s.Summarize(context.Background(), "the transcript")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if ins.Summary != "sum" || len(ins.ActionItems) != 1 || ms.gotTranscript != "the transcript" {
		t.Fatalf("Summarize delegation: got %+v transcript %q", ins, ms.gotTranscript)
	}
}

func TestServiceDelegatesErrors(t *testing.T) {
	wantErr := errors.New("boom")
	s := NewWithImpls(&mockTranscriber{err: wantErr}, &mockSummarizer{err: wantErr})
	if _, err := s.Transcribe(context.Background(), "x"); !errors.Is(err, wantErr) {
		t.Errorf("Transcribe error not propagated: %v", err)
	}
	if _, err := s.Summarize(context.Background(), "x"); !errors.Is(err, wantErr) {
		t.Errorf("Summarize error not propagated: %v", err)
	}
}

func TestParseInsightJSON(t *testing.T) {
	// Tolerates markdown fences / surrounding prose.
	in := "Here you go:\n```json\n{\"summary\":\"caller asked about billing\",\"action_items\":[\"email invoice\"]}\n```\nthanks"
	got, err := parseInsightJSON(in)
	if err != nil {
		t.Fatalf("parseInsightJSON: %v", err)
	}
	if got.Summary != "caller asked about billing" || len(got.ActionItems) != 1 || got.ActionItems[0] != "email invoice" {
		t.Fatalf("unexpected parse: %+v", got)
	}
	// Missing action_items → non-nil empty slice.
	got2, err := parseInsightJSON(`{"summary":"hi"}`)
	if err != nil || got2.ActionItems == nil {
		t.Fatalf("nil action_items should coerce to empty slice: %+v %v", got2, err)
	}
	// No JSON → error.
	if _, err := parseInsightJSON("no json here"); err == nil {
		t.Fatal("expected error for non-JSON reply")
	}
}

// mockServiceNil verifies a nil *Service is safe (mirrors nil sealer).
func TestNilServiceSafe(t *testing.T) {
	var s *Service
	if s.Enabled() || s.SummariesEnabled() {
		t.Fatal("nil service must report disabled")
	}
}
