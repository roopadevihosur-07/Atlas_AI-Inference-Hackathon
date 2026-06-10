# ATLAS — The Autonomous Campaign Agency

> One brief in. Fifty ads out. For every market you sell in. In ninety seconds.

ATLAS is a multi-agent system that turns a one-line brand brief into a matrix of
localized social media ads. It uses **Akamai AI Inference Cloud** as the LLM
brain for three coordinated agents (Strategist, Copywriter, Art Director), and
**Magnific** as the creative engine for image generation and upscaling.

Built in one day at AI Inference Hack Day · June 10, 2026.

---

## The pitch (90 seconds)

**The problem.** Meta's algorithm rewards creative volume — brands that test 50
ad variants per week beat brands that test 5. The catch: 95% of teams can't
produce that volume, and they certainly can't localize it for 30 markets.
Creative is now the bottleneck of paid media.

**The product.** ATLAS is the autonomous performance creative team. You give
it a one-line brief. Three coordinated agents — Strategist, Copywriter, Art
Director — decompose the brief into psychologically distinct angles and
culturally adapted markets, then produce a full ad for every cell of the
matrix in parallel. You watch it happen live.

**The wow.** Watch a 4×3 grid (or 7×5, or 10×8) fill in cell by cell on stage.
Each cell is a real, on-brand, market-specific ad. Built in seconds, not weeks.

**The market.** ~$500B global digital ad spend. Performance teams at Shopify
Plus, DTC brands, agencies, and growth-stage SaaS will buy this tomorrow.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  Browser (JS)                                                │
│  - Brief input form                                          │
│  - Live-filling matrix grid                                  │
│  - Agent activity log (monospace stream)                     │
└──────────────────┬──────────────────────────────────────────┘
                   │ POST /api/campaign, then SSE stream
                   ▼
┌─────────────────────────────────────────────────────────────┐
│  Go server (net/http + SSE)                                  │
│                                                              │
│  1. Strategist Agent ──► Akamai LLM                          │
│     Brief → { angles: [...], markets: [...] }                │
│                                                              │
│  2. For each (angle × market), in parallel goroutines:       │
│     ┌─ Copywriter Agent  ──► Akamai LLM (localized copy)     │
│     ├─ Art Director Agent ─► Akamai LLM (image prompt)       │
│     ├─ Magnific text-to-image  (generate)                    │
│     └─ Magnific upscaler       (upscale to print-ready)      │
│                                                              │
│  3. SSE-stream each completed cell to the browser            │
└─────────────────────────────────────────────────────────────┘
```

Default matrix: **4 angles × 3 markets = 12 ads**. Configurable via the brief.

---

## Quickstart

### 1. Prereqs
- Go 1.22+
- An Akamai AI Inference Cloud API key (you'll get this at the workshop)
- A Magnific / Freepik API key (https://magnific.ai/api or freepik.com/api)

### 2. Configure
```bash
cp .env.example .env
# Edit .env and fill in your keys
```

### 3. Run
```bash
go run .
# Server starts on http://localhost:8080
```

### 4. Open
http://localhost:8080 — type your brief, hit launch, watch the matrix fill.

---

## Budget guide

Akamai $300 of inference credit is effectively unlimited for this build —
a full campaign run uses roughly 16K tokens (~$0.03–0.10 of inference).

Magnific is your real constraint. Rough cost per cell at default settings:

| Step | Credits per cell (approx) |
|---|---|
| Flux Dev image generation | ~50 |
| 2× Magnific upscale | ~50–100 |
| **Total per cell** | **~100–150** |

A default 4 × 3 = 12-cell campaign costs roughly **1,200–1,800 credits**.
30K credits ≈ **15–25 full runs**, comfortably.

### Cost-saving workflow

1. **UI iteration** — set `MOCK_IMAGES=true` in `.env`. Uses picsum
   placeholders, burns zero Magnific credits. Use this for 90% of dev.
2. **Pipeline validation** — once the UI is wired up, set
   `MOCK_IMAGES=false` and `SKIP_UPSCALE=true`. Validates the real
   text-to-image call at half the credit cost.
3. **Full quality** — both flags false. Use this only for rehearsals and
   the live stage demo.
4. **Stretch your demo runs** — switch `MAGNIFIC_T2I_MODEL` to
   `flux-schnell` for ~5× cheaper image generation if you need more
   demo runs.

---

## Demo script (3 minutes on stage)

**[0:00]** "Performance marketers tell us their #1 constraint isn't budget —
it's creative volume. Meta wants 50 ads. They can make 5. Worse, they can
only do it in one language."

**[0:15]** "Meet ATLAS — the autonomous campaign agency." Pull up the UI.

**[0:25]** "I'm going to type one sentence about a real brand." Type the
judge's company in the brief box, or pick a Y Combinator startup.

**[0:35]** "Launch." The Strategist agent's reasoning streams in the log
panel. Four angles appear. Three markets appear. The 12-cell matrix
skeleton renders.

**[0:50–2:00]** Cells fill in live. Each cell shows the Copywriter's text
(in the local language), the Art Director's prompt, then the generated
image fades in, then the Magnific upscale ping confirms.

**[2:15]** "Twelve ads. Four psychological angles. Three markets. Three
languages. Sixty seconds of compute. Zero humans involved." Pause for
the room.

**[2:30]** "Wedge: 1,000 Shopify Plus brands burning cash on stagnant
creative. Scale: every brand on earth has this problem. We're live tonight
at atlas.[domain]."

---

## What's next (post-hackathon)

- Brand kit memory (logo, palette, voice doc) for true on-brand output
- Direct publishing to Meta Ads Manager + TikTok Ads via API
- Performance feedback loop: ATLAS learns which angles win
- Video ads via Magnific's Kling / Veo endpoints
- Custom market sets per industry (B2B SaaS markets ≠ DTC fashion markets)

---

## File map

```
atlas/
├── main.go          # HTTP server, SSE endpoint, routes
├── agent.go         # Multi-agent orchestration
├── akamai.go        # Akamai LLM client (OpenAI-compatible)
├── magnific.go      # Magnific / Freepik API client
├── types.go         # Shared types
├── go.mod
├── .env.example
├── static/
│   ├── index.html   # The mission-control UI
│   ├── style.css    # Dark, dense, deliberate
│   └── app.js       # SSE consumer + matrix renderer
└── README.md
```
