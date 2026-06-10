package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// MagnificClient talks to the Magnific / Freepik AI API.
// Both endpoints are async: submit → task ID → poll until "completed".
type MagnificClient struct {
	baseURL       string
	apiKey        string
	t2iModel      string
	upscaleLevel  int
	upscaleFactor int
	mock          bool
	skipUpscale   bool
	http          *http.Client
}

func NewMagnificClient(cfg Config) *MagnificClient {
	return &MagnificClient{
		baseURL:       strings.TrimRight(cfg.MagnificBase, "/"),
		apiKey:        cfg.MagnificKey,
		t2iModel:      cfg.T2IModel,
		upscaleLevel:  cfg.UpscaleLevel,
		upscaleFactor: cfg.UpscaleFactor,
		mock:          cfg.MockImages,
		skipUpscale:   cfg.SkipUpscale,
		http:          &http.Client{Timeout: 120 * time.Second},
	}
}

// taskAck is the shape of the initial submit response.
type taskAck struct {
	Data struct {
		TaskID     string   `json:"task_id"`
		Status     string   `json:"status"`
		Generated  []string `json:"generated,omitempty"`
	} `json:"data"`
}

// taskPoll is what we get back from the polling endpoint.
type taskPoll struct {
	Data struct {
		TaskID    string   `json:"task_id"`
		Status    string   `json:"status"` // "CREATED" | "IN_PROGRESS" | "COMPLETED" | "FAILED"
		Generated []string `json:"generated,omitempty"`
	} `json:"data"`
}

func (m *MagnificClient) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, m.baseURL+path, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-freepik-api-key", m.apiKey)

	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("magnific HTTP %d on %s: %s", resp.StatusCode, path, string(raw))
	}
	return raw, nil
}

// GenerateImage submits a text-to-image task and polls until completion.
// Returns the first generated image URL.
func (m *MagnificClient) GenerateImage(ctx context.Context, prompt, negative string) (string, error) {
	// Mock mode: pick from a curated pool of verified baby/parenting Unsplash
	// photos. Each cell gets a different image via a hash of its prompt.
	if m.mock {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
		pool := []string{
			"photo-1496174742515-d2146dcf8e80",
			"photo-1546015720-b8b30df5aa27",
			"photo-1582212742235-a2500f31cb39",
			"photo-1587116215900-bb2bba7c7cff",
			"photo-1510632233616-88025944e960",
			"photo-1481728236344-b5c828da9edf",
			"photo-1583086762675-5a88bcc72548",
			"photo-1543346242-2b8e41fb91ca",
			"photo-1608586769800-7b7dc96aeb6a",
			"photo-1552819289-e14fbbcea868",
		}
		idx := hashInt(prompt) % len(pool)
		return fmt.Sprintf("https://images.unsplash.com/%s?w=768&h=768&fit=crop&q=80", pool[idx]), nil
	}

	path := "/ai/text-to-image/" + m.t2iModel

	submitBody := map[string]any{
		"prompt":          prompt,
		"negative_prompt": negative,
		"aspect_ratio":    "square_1_1",
		"num_images":      1,
	}

	raw, err := m.do(ctx, "POST", path, submitBody)
	if err != nil {
		return "", fmt.Errorf("t2i submit: %w", err)
	}

	var ack taskAck
	if err := json.Unmarshal(raw, &ack); err != nil {
		return "", fmt.Errorf("t2i ack parse: %w", err)
	}

	// Some endpoints return the image inline if it's fast enough.
	if len(ack.Data.Generated) > 0 {
		return ack.Data.Generated[0], nil
	}

	return m.pollUntilDone(ctx, path+"/"+ack.Data.TaskID)
}

// Upscale submits an image upscaling task (the actual Magnific upscaler)
// and polls until completion. Returns the upscaled image URL.
func (m *MagnificClient) Upscale(ctx context.Context, imageURL, guidancePrompt string) (string, error) {
	// Skip path: mock mode or explicit SKIP_UPSCALE — pass the source through.
	if m.mock || m.skipUpscale {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(600 * time.Millisecond):
		}
		return imageURL, nil
	}

	path := "/ai/image-upscaler"

	submitBody := map[string]any{
		"image":       imageURL,
		"scale_factor": fmt.Sprintf("%dx", m.upscaleFactor),
		"creativity":  m.upscaleLevel,
		"prompt":      guidancePrompt, // optional creative guidance
		"engine":      "magnific_sparkle",
	}

	raw, err := m.do(ctx, "POST", path, submitBody)
	if err != nil {
		return "", fmt.Errorf("upscale submit: %w", err)
	}

	var ack taskAck
	if err := json.Unmarshal(raw, &ack); err != nil {
		return "", fmt.Errorf("upscale ack parse: %w", err)
	}
	if len(ack.Data.Generated) > 0 {
		return ack.Data.Generated[0], nil
	}
	return m.pollUntilDone(ctx, path+"/"+ack.Data.TaskID)
}

// hashInt returns a deterministic positive integer from s (used as a LoremFlickr lock).
func hashInt(s string) int {
	sum := sha1.Sum([]byte(s))
	return int(sum[0])<<16 | int(sum[1])<<8 | int(sum[2])
}

// hashString kept for any callers that still need the hex form.
func hashString(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}


// pollUntilDone polls a Magnific task endpoint until it's COMPLETED or fails.
func (m *MagnificClient) pollUntilDone(ctx context.Context, pollPath string) (string, error) {
	deadline := time.Now().Add(110 * time.Second)
	delay := 1500 * time.Millisecond

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(delay):
		}

		raw, err := m.do(ctx, "GET", pollPath, nil)
		if err != nil {
			return "", fmt.Errorf("poll: %w", err)
		}

		var poll taskPoll
		if err := json.Unmarshal(raw, &poll); err != nil {
			return "", fmt.Errorf("poll parse: %w", err)
		}

		switch poll.Data.Status {
		case "COMPLETED":
			if len(poll.Data.Generated) == 0 {
				return "", fmt.Errorf("poll: completed but no images")
			}
			return poll.Data.Generated[0], nil
		case "FAILED":
			return "", fmt.Errorf("magnific task failed")
		}

		// Gentle backoff up to 3s
		if delay < 3*time.Second {
			delay += 500 * time.Millisecond
		}
	}
	return "", fmt.Errorf("magnific poll timeout")
}
