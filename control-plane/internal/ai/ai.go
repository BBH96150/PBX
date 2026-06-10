// Package ai provides config-gated call-recording transcription and AI call
// summaries (+ action items) for the insights pipeline.
//
// DISABLED BY DEFAULT. Exactly like crypto.NewSealerFromEnv (2FA) and
// sso.NewSAMLKeypairFromEnv (SAML): when no provider/keys are configured, New
// returns a Service whose Enabled() is false and whose Transcribe/Summarize
// are inert no-ops. The pipeline worker checks Enabled() and returns
// immediately when off, so NO call/voicemail behavior changes until an operator
// sets AI_TRANSCRIPTION_PROVIDER + the relevant API keys.
//
// The only part that cannot be exercised without live keys is the provider HTTP
// transport in deepgram.go / anthropic.go — those are flagged there and behind
// the Transcriber/Summarizer interfaces so everything else is unit-testable
// with mocks.
package ai

import (
	"context"
	"log/slog"
)

// Insight is the AI-generated result for one call: a short summary and a list
// of action items extracted from the transcript.
type Insight struct {
	Summary     string   `json:"summary"`
	ActionItems []string `json:"action_items"`
}

// Transcriber turns an audio file into a plain-text transcript.
type Transcriber interface {
	Transcribe(ctx context.Context, audioPath string) (string, error)
}

// Summarizer turns a transcript into a structured Insight (summary + actions).
type Summarizer interface {
	Summarize(ctx context.Context, transcript string) (Insight, error)
}

// Config selects the provider implementations. Built from config.Config in
// main.go. An empty/partial Config yields a disabled Service.
type Config struct {
	// TranscriptionProvider is "deepgram" or "" (empty disables everything).
	TranscriptionProvider string
	DeepgramAPIKey        string
	// AnthropicAPIKey enables summaries. Transcription can run without it; in
	// that case the worker stores transcripts but no summaries.
	AnthropicAPIKey string
}

// Service is the façade the pipeline worker depends on. It delegates to the
// injected Transcriber/Summarizer. When disabled both are nil.
type Service struct {
	transcriber Transcriber
	summarizer  Summarizer
}

// New builds a Service from cfg. Config-gated, mirroring NewSealerFromEnv: with
// no provider/key it returns a disabled Service (Enabled()==false) and wires no
// real provider clients. Transcription needs the provider + its key; summaries
// additionally need the Anthropic key.
func New(cfg Config) *Service {
	s := &Service{}
	switch cfg.TranscriptionProvider {
	case "deepgram":
		if cfg.DeepgramAPIKey != "" {
			s.transcriber = NewDeepgramTranscriber(cfg.DeepgramAPIKey)
		}
	}
	if cfg.AnthropicAPIKey != "" {
		s.summarizer = NewAnthropicSummarizer(cfg.AnthropicAPIKey)
	}
	return s
}

// NewWithImpls injects explicit implementations. Used by tests (with mocks) and
// available to callers that wire their own providers. Either may be nil.
func NewWithImpls(t Transcriber, sum Summarizer) *Service {
	return &Service{transcriber: t, summarizer: sum}
}

// Enabled reports whether transcription is configured. Summaries additionally
// require SummariesEnabled(). When false, the pipeline worker is inert.
func (s *Service) Enabled() bool {
	return s != nil && s.transcriber != nil
}

// SummariesEnabled reports whether call summaries can be produced (Anthropic
// key set). Transcription may be enabled without summaries.
func (s *Service) SummariesEnabled() bool {
	return s != nil && s.summarizer != nil
}

// Transcribe delegates to the injected Transcriber. Returns an empty string
// (no error) when transcription is disabled so callers can treat it as "no
// transcript available" without special-casing.
func (s *Service) Transcribe(ctx context.Context, audioPath string) (string, error) {
	if !s.Enabled() {
		return "", nil
	}
	return s.transcriber.Transcribe(ctx, audioPath)
}

// Summarize delegates to the injected Summarizer. Returns a zero Insight (no
// error) when summaries are disabled.
func (s *Service) Summarize(ctx context.Context, transcript string) (Insight, error) {
	if !s.SummariesEnabled() {
		return Insight{}, nil
	}
	return s.summarizer.Summarize(ctx, transcript)
}

// LogStatus emits a single line describing the enabled/disabled state. Called
// once at worker startup.
func (s *Service) LogStatus() {
	switch {
	case !s.Enabled():
		slog.Info("AI insights disabled (set AI_TRANSCRIPTION_PROVIDER + keys to enable)")
	case !s.SummariesEnabled():
		slog.Info("AI insights: transcription enabled, summaries disabled (set ANTHROPIC_API_KEY to enable)")
	default:
		slog.Info("AI insights: transcription + summaries enabled")
	}
}
