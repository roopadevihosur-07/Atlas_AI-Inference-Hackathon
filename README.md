# ATLAS — The Autonomous Campaign Agency

> One brief in. Fifty ads out. For every market you sell in. In ninety seconds.

ATLAS is a multi-agent system that turns a one-line brand brief into a matrix of
localized social media ads. It uses **Akamai AI Inference Cloud** as the LLM
brain for three coordinated agents (Strategist, Copywriter, Art Director), and
**Magnific** as the creative engine for image generation and upscaling.

Built in one day at AI Inference Hack Day · June 10, 2026.

---

## Brief Points

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
