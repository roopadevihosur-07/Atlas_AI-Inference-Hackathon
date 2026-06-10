package main

import (
	"encoding/json"
	"strings"
	"time"
)

// ── Configuration ────────────────────────────────────────────────────────────

type Config struct {
	AkamaiBaseURL string
	AkamaiAPIKey  string
	AkamaiModel   string
	MagnificBase  string
	MagnificKey   string
	T2IModel      string
	UpscaleLevel  int
	UpscaleFactor int
	Port          string
	MaxParallel   int

	// Cost-saving flags for dev iteration.
	MockImages   bool // true → return picsum placeholders, no Magnific calls
	SkipUpscale  bool // true → skip the upscale step, use raw gen image
}

// ── Campaign matrix ──────────────────────────────────────────────────────────

type Brief struct {
	Text       string `json:"text"`        // one-line brand brief
	NumAngles  int    `json:"num_angles"`  // default 4
	NumMarkets int    `json:"num_markets"` // default 3
}

type Angle struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Hypothesis string `json:"hypothesis"`
}

type Market struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Language      string `json:"language"`
	CulturalNotes string `json:"cultural_notes"`
}

// UnmarshalJSON handles cultural_notes as either a plain string or an array of
// strings — some models return ["note1", "note2"] despite being asked for a string.
func (m *Market) UnmarshalJSON(b []byte) error {
	var aux struct {
		ID            string          `json:"id"`
		Name          string          `json:"name"`
		Language      string          `json:"language"`
		CulturalNotes json.RawMessage `json:"cultural_notes"`
	}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	m.ID = aux.ID
	m.Name = aux.Name
	m.Language = aux.Language

	var s string
	if json.Unmarshal(aux.CulturalNotes, &s) == nil {
		m.CulturalNotes = s
		return nil
	}
	var arr []string
	if json.Unmarshal(aux.CulturalNotes, &arr) == nil {
		m.CulturalNotes = strings.Join(arr, "; ")
		return nil
	}
	return nil // best-effort: leave CulturalNotes empty rather than hard-fail
}

type Strategy struct {
	ProductName string   `json:"product_name"`
	Tagline     string   `json:"tagline"`
	Angles      []Angle  `json:"angles"`
	Markets     []Market `json:"markets"`
}

type Copy struct {
	Headline string `json:"headline"`
	Body     string `json:"body"`
	CTA      string `json:"cta"`
	Language string `json:"language"`
}

type ArtBrief struct {
	Prompt         string `json:"prompt"`
	NegativePrompt string `json:"negative_prompt"`
}

// A single cell in the campaign matrix: one (angle × market) ad.
type Cell struct {
	AngleID    string    `json:"angle_id"`
	MarketID   string    `json:"market_id"`
	Status     string    `json:"status"` // "pending" | "writing" | "directing" | "rendering" | "upscaling" | "done" | "error"
	Copy       *Copy     `json:"copy,omitempty"`
	Art        *ArtBrief `json:"art,omitempty"`
	ImageURL   string    `json:"image_url,omitempty"`
	UpscaleURL string    `json:"upscale_url,omitempty"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"-"`
	ElapsedMs  int64     `json:"elapsed_ms,omitempty"`
}

// ── SSE event envelope ───────────────────────────────────────────────────────

// Event is everything the frontend needs to update the UI as the agent works.
type Event struct {
	Type     string      `json:"type"` // "log" | "strategy" | "cell" | "done" | "error"
	Agent    string      `json:"agent,omitempty"`
	Message  string      `json:"message,omitempty"`
	Strategy *Strategy   `json:"strategy,omitempty"`
	Cell     *Cell       `json:"cell,omitempty"`
	Stats    *RunStats   `json:"stats,omitempty"`
}

type RunStats struct {
	TotalCells int   `json:"total_cells"`
	Completed  int   `json:"completed"`
	ElapsedMs  int64 `json:"elapsed_ms"`
}
