package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AkamaiClient talks to any OpenAI-compatible Chat Completions endpoint.
// Akamai AI Inference Cloud exposes one; so do vLLM, NVIDIA NIMs, etc.
type AkamaiClient struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

func NewAkamaiClient(cfg Config) *AkamaiClient {
	return &AkamaiClient{
		baseURL: strings.TrimRight(cfg.AkamaiBaseURL, "/"),
		apiKey:  cfg.AkamaiAPIKey,
		model:   cfg.AkamaiModel,
		http:    &http.Client{Timeout: 300 * time.Second},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	// Some OpenAI-compatible servers ignore this; many honor it.
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"` // "json_object"
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Chat sends a system+user prompt and returns the assistant text.
// If wantJSON is true, we request and gently coax JSON-only output.
func (c *AkamaiClient) Chat(ctx context.Context, system, user string, wantJSON bool) (string, error) {
	req := chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: 0.7,
		MaxTokens:   400,
	}
	if wantJSON {
		req.ResponseFormat = &responseFormat{Type: "json_object"}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("akamai request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("akamai HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("akamai parse: %w (raw: %s)", err, string(raw))
	}
	if out.Error != nil {
		return "", fmt.Errorf("akamai api error: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("akamai: no choices in response")
	}
	return out.Choices[0].Message.Content, nil
}

// ChatJSON calls Chat and parses the JSON result into dst.
// Retries once on parse failure — small models occasionally wrap JSON in prose.
func (c *AkamaiClient) ChatJSON(ctx context.Context, system, user string, dst any) error {
	// Append a hard reminder so smaller models don't add preamble.
	userWithHint := user + "\n\nIMPORTANT: respond with ONLY the JSON object. Start with { and end with }. No other text."
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		raw, err := c.Chat(ctx, system, userWithHint, true)
		if err != nil {
			return err
		}
		clean := extractJSON(raw)
		if err := json.Unmarshal([]byte(clean), dst); err == nil {
			return nil
		} else {
			lastErr = fmt.Errorf("akamai JSON parse: %w (raw: %s)", err, clean)
		}
	}
	return lastErr
}

// extractJSON strips markdown fences and any prose before/after the JSON
// object — smaller models frequently add "Sure! Here's the JSON:" preambles.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	for _, pfx := range []string{"```json\n", "```json", "```\n", "```"} {
		if strings.HasPrefix(s, pfx) {
			s = s[len(pfx):]
			break
		}
	}
	if idx := strings.LastIndex(s, "```"); idx >= 0 {
		s = s[:idx]
	}
	s = strings.TrimSpace(s)
	// Trim any prose before the first { and after the last }
	if i := strings.Index(s, "{"); i > 0 {
		s = s[i:]
	}
	if i := strings.LastIndex(s, "}"); i >= 0 && i < len(s)-1 {
		s = s[:i+1]
	}
	return strings.TrimSpace(s)
}
