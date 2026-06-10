package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// DeepgramTranscriber posts a recording to Deepgram's pre-recorded speech-to-
// text API and returns the transcript.
//
// LIVE VALIDATION REQUIRED: this is the ONLY part of the AI feature that cannot
// be exercised without a real DEEPGRAM_API_KEY. The request/response shape
// below matches Deepgram's documented /v1/listen pre-recorded endpoint
// (results.channels[0].alternatives[0].transcript) as of this writing, but the
// exact wire contract should be confirmed against a live key before relying on
// it in production. Everything that consumes a *DeepgramTranscriber goes
// through the Transcriber interface and is mock-tested.
type DeepgramTranscriber struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewDeepgramTranscriber returns a transcriber bound to the given key. Only
// constructed by ai.New when AI_TRANSCRIPTION_PROVIDER=deepgram and the key is
// set, so apiKey is always non-empty here.
func NewDeepgramTranscriber(apiKey string) *DeepgramTranscriber {
	return &DeepgramTranscriber{
		apiKey:  apiKey,
		baseURL: "https://api.deepgram.com/v1/listen",
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

// deepgramResponse is the subset of the JSON response we parse.
type deepgramResponse struct {
	Results struct {
		Channels []struct {
			Alternatives []struct {
				Transcript string `json:"transcript"`
			} `json:"alternatives"`
		} `json:"channels"`
	} `json:"results"`
}

// Transcribe streams the audio file at audioPath to Deepgram and returns the
// best-alternative transcript. The caller is responsible for having already
// validated audioPath against the storage-root path-traversal guard.
func (d *DeepgramTranscriber) Transcribe(ctx context.Context, audioPath string) (string, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return "", fmt.Errorf("deepgram: open audio: %w", err)
	}
	defer f.Close()

	// smart_format gives punctuation/capitalization; punctuate=true for safety.
	url := d.baseURL + "?smart_format=true&punctuate=true"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, f)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Token "+d.apiKey)
	// Deepgram sniffs the container from the bytes when Content-Type is generic.
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("deepgram: request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("deepgram: status %d", resp.StatusCode)
	}

	var dr deepgramResponse
	if err := json.NewDecoder(resp.Body).Decode(&dr); err != nil {
		return "", fmt.Errorf("deepgram: decode: %w", err)
	}
	if len(dr.Results.Channels) == 0 || len(dr.Results.Channels[0].Alternatives) == 0 {
		return "", nil // no speech detected; not an error
	}
	return dr.Results.Channels[0].Alternatives[0].Transcript, nil
}
