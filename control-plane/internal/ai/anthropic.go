package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// AnthropicSummarizer asks the Anthropic Messages API to produce a short
// summary + action items for a call transcript, returned as JSON.
//
// LIVE VALIDATION REQUIRED: like deepgram.go, the provider HTTP call here is the
// only un-testable-without-keys part. The request shape matches the documented
// /v1/messages API (model, max_tokens, messages, anthropic-version header) and
// we ask the model to reply with strict JSON that we parse. Confirm against a
// live ANTHROPIC_API_KEY before production use. Consumers go through the
// Summarizer interface and are mock-tested.
type AnthropicSummarizer struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

// NewAnthropicSummarizer returns a summarizer bound to the given key. Only
// constructed by ai.New when ANTHROPIC_API_KEY is set.
func NewAnthropicSummarizer(apiKey string) *AnthropicSummarizer {
	return &AnthropicSummarizer{
		apiKey:  apiKey,
		baseURL: "https://api.anthropic.com/v1/messages",
		model:   "claude-3-5-haiku-latest",
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

const summaryPrompt = `You are summarizing a phone call transcript for a business phone system. ` +
	`Respond with ONLY a JSON object, no markdown, no prose, in exactly this shape:
{"summary": "<2-3 sentence summary of the call>", "action_items": ["<item>", "..."]}
If there are no action items, use an empty array. Transcript:

`

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// Summarize sends the transcript and parses the model's JSON reply into an
// Insight. An empty transcript yields a zero Insight without calling the API.
func (a *AnthropicSummarizer) Summarize(ctx context.Context, transcript string) (Insight, error) {
	if strings.TrimSpace(transcript) == "" {
		return Insight{}, nil
	}
	reqBody, err := json.Marshal(anthropicRequest{
		Model:     a.model,
		MaxTokens: 1024,
		Messages: []anthropicMessage{
			{Role: "user", Content: summaryPrompt + transcript},
		},
	})
	if err != nil {
		return Insight{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL, bytes.NewReader(reqBody))
	if err != nil {
		return Insight{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.client.Do(req)
	if err != nil {
		return Insight{}, fmt.Errorf("anthropic: request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return Insight{}, fmt.Errorf("anthropic: status %d", resp.StatusCode)
	}

	var ar anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return Insight{}, fmt.Errorf("anthropic: decode: %w", err)
	}
	var text strings.Builder
	for _, c := range ar.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	return parseInsightJSON(text.String())
}

// parseInsightJSON extracts the {"summary","action_items"} object from the
// model's reply, tolerating leading/trailing prose or markdown fences by
// slicing to the outermost braces. Exported-ish helper kept package-private but
// pure, so it's unit-testable without the network.
func parseInsightJSON(s string) (Insight, error) {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end < start {
		return Insight{}, fmt.Errorf("anthropic: no JSON object in reply")
	}
	var ins Insight
	if err := json.Unmarshal([]byte(s[start:end+1]), &ins); err != nil {
		return Insight{}, fmt.Errorf("anthropic: parse insight: %w", err)
	}
	if ins.ActionItems == nil {
		ins.ActionItems = []string{}
	}
	return ins, nil
}
