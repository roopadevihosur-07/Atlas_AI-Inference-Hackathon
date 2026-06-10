package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	cfg := loadConfig()
	mustHaveKeys(cfg)

	agent := NewAgent(cfg)

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir("./static")))
	mux.HandleFunc("/api/campaign", func(w http.ResponseWriter, r *http.Request) {
		handleCampaign(w, r, agent)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: logRequests(mux),
	}

	go func() {
		log.Printf("ATLAS up on http://localhost:%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	log.Println("ATLAS down")
}

// ── /api/campaign: POST → SSE stream ─────────────────────────────────────────

func handleCampaign(w http.ResponseWriter, r *http.Request, agent *Agent) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	var brief Brief
	if err := json.NewDecoder(r.Body).Decode(&brief); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(brief.Text) == "" {
		http.Error(w, "empty brief", http.StatusBadRequest)
		return
	}

	// SSE headers — critical for streaming through any intermediate proxies.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	events := make(chan Event, 32)
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()

	go agent.Run(ctx, brief, events)

	// Stream each event as an SSE frame. Flush after every frame so the
	// browser sees cells fill in live.
	for ev := range events {
		b, err := json.Marshal(ev)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}
}

// ── Config & helpers ─────────────────────────────────────────────────────────

func loadConfig() Config {
	loadDotEnv(".env")
	cfg := Config{
		AkamaiBaseURL: getenv("AKAMAI_BASE_URL", ""),
		AkamaiAPIKey:  getenv("AKAMAI_API_KEY", ""),
		AkamaiModel:   getenv("AKAMAI_MODEL", "llama-3.3-70b-instruct"),
		MagnificBase:  getenv("MAGNIFIC_BASE_URL", "https://api.freepik.com/v1"),
		MagnificKey:   getenv("MAGNIFIC_API_KEY", ""),
		T2IModel:      getenv("MAGNIFIC_T2I_MODEL", "flux-dev"),
		UpscaleLevel:  atoi(getenv("MAGNIFIC_UPSCALE_CREATIVITY", "4")),
		UpscaleFactor: atoi(getenv("MAGNIFIC_UPSCALE_FACTOR", "2")),
		Port:          getenv("PORT", "8080"),
		MaxParallel:   atoi(getenv("MAX_PARALLEL_CELLS", "4")),
		MockImages:    getenv("MOCK_IMAGES", "false") == "true",
		SkipUpscale:   getenv("SKIP_UPSCALE", "false") == "true",
	}
	return cfg
}

func mustHaveKeys(cfg Config) {
	missing := []string{}
	if cfg.AkamaiAPIKey == "" {
		missing = append(missing, "AKAMAI_API_KEY")
	}
	if cfg.AkamaiBaseURL == "" {
		missing = append(missing, "AKAMAI_BASE_URL")
	}
	// In mock mode we don't need a Magnific key.
	if !cfg.MockImages && cfg.MagnificKey == "" {
		missing = append(missing, "MAGNIFIC_API_KEY")
	}
	if len(missing) > 0 {
		log.Fatalf("missing required env vars: %v (copy .env.example to .env and fill them in)", missing)
	}
	if cfg.MockImages {
		log.Println("⚠  MOCK_IMAGES=true — using picsum placeholders, no Magnific credits will be spent")
	}
	if cfg.SkipUpscale {
		log.Println("⚠  SKIP_UPSCALE=true — upscaler disabled, demo grid uses raw gen images")
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// loadDotEnv is a tiny .env loader so we don't need a third-party dep.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.TrimSpace(line[eq+1:])
		v = strings.Trim(v, `"'`)
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start))
	})
}
