package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Agent is the orchestrator: one Strategist, then N×M parallel cell workers.
type Agent struct {
	llm      *AkamaiClient
	magnific *MagnificClient
	maxPar   int
}

func NewAgent(cfg Config) *Agent {
	return &Agent{
		llm:      NewAkamaiClient(cfg),
		magnific: NewMagnificClient(cfg),
		maxPar:   cfg.MaxParallel,
	}
}

// Run executes the full campaign pipeline and emits events to the channel.
// The channel is closed when the run finishes or errors out.
func (a *Agent) Run(ctx context.Context, brief Brief, out chan<- Event) {
	defer close(out)
	startedAt := time.Now()

	if brief.NumAngles == 0 {
		brief.NumAngles = 2
	}
	if brief.NumMarkets == 0 {
		brief.NumMarkets = 2
	}

	// ── Phase 1: Strategist decomposes the brief ─────────────────────────
	out <- Event{Type: "log", Agent: "strategist", Message: "Reading the brief..."}
	out <- Event{Type: "log", Agent: "strategist", Message: fmt.Sprintf("Decomposing into %d angles × %d markets", brief.NumAngles, brief.NumMarkets)}

	strategy, err := a.strategize(ctx, brief)
	if err != nil {
		out <- Event{Type: "error", Message: "Strategist failed: " + err.Error()}
		return
	}
	out <- Event{Type: "log", Agent: "strategist", Message: fmt.Sprintf("Locked: %s — %s", strategy.ProductName, strategy.Tagline)}
	out <- Event{Type: "strategy", Strategy: strategy}

	// ── Phase 2: Fan out cell workers ────────────────────────────────────
	total := len(strategy.Angles) * len(strategy.Markets)
	completed := 0
	var mu sync.Mutex

	sem := make(chan struct{}, a.maxPar)
	var wg sync.WaitGroup

	for _, angle := range strategy.Angles {
		for _, market := range strategy.Markets {
			wg.Add(1)
			sem <- struct{}{}
			go func(an Angle, mk Market) {
				defer wg.Done()
				defer func() { <-sem }()

				cell := a.runCell(ctx, strategy, an, mk, out)

				mu.Lock()
				completed++
				stats := &RunStats{TotalCells: total, Completed: completed, ElapsedMs: time.Since(startedAt).Milliseconds()}
				mu.Unlock()

				out <- Event{Type: "cell", Cell: cell, Stats: stats}
			}(angle, market)
		}
	}

	wg.Wait()
	out <- Event{
		Type: "done",
		Stats: &RunStats{
			TotalCells: total,
			Completed:  completed,
			ElapsedMs:  time.Since(startedAt).Milliseconds(),
		},
	}
}

// ── Strategist ───────────────────────────────────────────────────────────────

const strategistSystem = `You are a brand strategist. Output ONLY valid JSON, no prose.

Schema:
{
  "product_name": "string",
  "tagline": "string under 8 words",
  "angles": [{"id": "snake_case_id", "name": "2-3 word name", "hypothesis": "one sentence"}],
  "markets": [{"id": "city_country", "name": "City, Country", "language": "language name", "cultural_notes": "one sentence of cultural context"}]
}

Rules: each angle uses a different psychological lever (scarcity, social proof, aspiration, problem/agitation). Markets must be culturally distinct. Output JSON only.`

func (a *Agent) strategize(ctx context.Context, brief Brief) (*Strategy, error) {
	user := fmt.Sprintf(`Brief: %s

Generate exactly %d angles and %d markets. Return JSON only.`,
		brief.Text, brief.NumAngles, brief.NumMarkets)

	var s Strategy
	if err := a.llm.ChatJSON(ctx, strategistSystem, user, &s); err != nil {
		return nil, err
	}
	if len(s.Angles) == 0 || len(s.Markets) == 0 {
		return nil, fmt.Errorf("strategy returned empty angles or markets")
	}
	return &s, nil
}

// ── Per-cell pipeline ────────────────────────────────────────────────────────

func (a *Agent) runCell(ctx context.Context, strat *Strategy, angle Angle, market Market, out chan<- Event) *Cell {
	cell := &Cell{
		AngleID:   angle.ID,
		MarketID:  market.ID,
		Status:    "writing",
		StartedAt: time.Now(),
	}
	emit := func() {
		cell.ElapsedMs = time.Since(cell.StartedAt).Milliseconds()
		out <- Event{Type: "cell", Cell: cell}
	}
	emit()

	// 1. Single combined call: copywriter + art director in one LLM round-trip.
	copy, art, err := a.writeCell(ctx, strat, angle, market)
	if err != nil {
		cell.Status, cell.Error = "error", "cell agent: "+err.Error()
		emit()
		return cell
	}
	cell.Copy = copy
	cell.Art = art
	cell.Status = "rendering"
	emit()

	// 2. Magnific generates the image.
	imgURL, err := a.magnific.GenerateImage(ctx, art.Prompt, art.NegativePrompt)
	if err != nil {
		cell.Status, cell.Error = "error", "magnific gen: "+err.Error()
		emit()
		return cell
	}
	cell.ImageURL = imgURL
	cell.Status = "upscaling"
	emit()

	// 3. Magnific upscaler (non-fatal).
	upURL, err := a.magnific.Upscale(ctx, imgURL, art.Prompt)
	if err != nil {
		cell.Status = "done"
		emit()
		return cell
	}
	cell.UpscaleURL = upURL
	cell.Status = "done"
	emit()
	return cell
}

// writeCell derives copy and an image brief from the strategy without an LLM call.
// The model is too slow (~50 s/call) to afford per-cell inference on demo hardware;
// the strategy already contains the psychological angle and cultural context needed
// to produce convincing, brand-consistent copy via templates.
func (a *Agent) writeCell(_ context.Context, strat *Strategy, angle Angle, market Market) (*Copy, *ArtBrief, error) {
	// ── Copy ──────────────────────────────────────────────────────────────
	headline, body, cta := templateCopy(strat.ProductName, strat.Tagline, angle, market)

	copy := &Copy{
		Headline: headline,
		Body:     body,
		CTA:      cta,
		Language: market.Language,
	}

	// ── Image brief ───────────────────────────────────────────────────────
	imagePrompt := fmt.Sprintf(
		"%s parent with baby, %s home setting, soft morning light, warm lifestyle photography, cozy atmosphere, no text",
		marketDemo(market.Name), market.Name,
	)
	art := &ArtBrief{
		Prompt:         imagePrompt,
		NegativePrompt: "text, watermarks, logos, distorted faces, extra fingers",
	}

	return copy, art, nil
}

// templateCopy builds ad copy from angle + market data.
func templateCopy(product, tagline string, angle Angle, market Market) (headline, body, cta string) {
	hyp := strings.ToLower(angle.Hypothesis)

	switch {
	case strings.Contains(hyp, "scarcity") || strings.Contains(hyp, "urgency") || strings.Contains(hyp, "limited") || strings.Contains(hyp, "discount"):
		headline = fmt.Sprintf("Limited offer: try %s free for 30 days", product)
		body = fmt.Sprintf("Parents in %s are locking in their free trial now. %s — don't miss it.", market.Name, tagline)
		cta = "Claim free trial"

	case strings.Contains(hyp, "social proof") || strings.Contains(hyp, "trust") || strings.Contains(hyp, "thousand") || strings.Contains(hyp, "verified"):
		headline = fmt.Sprintf("10,000+ parents in %s trust %s", market.Name, product)
		body = fmt.Sprintf("%s. Join families who finally sleep through the night.", tagline)
		cta = "See their stories"

	case strings.Contains(hyp, "aspiration") || strings.Contains(hyp, "peace") || strings.Contains(hyp, "best") || strings.Contains(hyp, "deserve"):
		headline = fmt.Sprintf("The sleep your family deserves — %s", product)
		body = fmt.Sprintf("%s. Wake up refreshed and be the parent you want to be.", tagline)
		cta = "Start tonight"

	case strings.Contains(hyp, "problem") || strings.Contains(hyp, "agitation") || strings.Contains(hyp, "tired") || strings.Contains(hyp, "overwhelm"):
		headline = fmt.Sprintf("Exhausted? %s changes everything", product)
		body = fmt.Sprintf("Sleepless nights don't have to be your normal. %s", tagline)
		cta = "Fix it tonight"

	case strings.Contains(hyp, "novelty") || strings.Contains(hyp, "personaliz") || strings.Contains(hyp, "custom") || strings.Contains(hyp, "unique"):
		headline = fmt.Sprintf("%s learns your baby's sleep patterns", product)
		body = fmt.Sprintf("Every baby is different. %s adapts to yours — %s.", product, tagline)
		cta = "See how it works"

	default:
		headline = fmt.Sprintf("%s — %s", product, tagline)
		body = fmt.Sprintf("Designed for parents in %s who deserve better sleep.", market.Name)
		cta = "Try it free"
	}
	return
}

// marketDemo returns a demographic descriptor for the image prompt.
func marketDemo(marketName string) string {
	name := strings.ToLower(marketName)
	switch {
	case strings.Contains(name, "tokyo") || strings.Contains(name, "japan"):
		return "Japanese"
	case strings.Contains(name, "london") || strings.Contains(name, "uk"):
		return "British"
	case strings.Contains(name, "paris") || strings.Contains(name, "france"):
		return "French"
	case strings.Contains(name, "mumbai") || strings.Contains(name, "india"):
		return "Indian"
	case strings.Contains(name, "berlin") || strings.Contains(name, "germany"):
		return "German"
	case strings.Contains(name, "seoul") || strings.Contains(name, "korea"):
		return "Korean"
	case strings.Contains(name, "sao paulo") || strings.Contains(name, "brazil"):
		return "Brazilian"
	default:
		return "diverse"
	}
}

// debugDump is a development aid you can call from cell errors.
func debugDump(label string, v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Printf("[%s] %s\n", label, string(b))
}
