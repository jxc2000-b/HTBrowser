package main

import (
	"errors"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"health-dash/pkg/internal/config"
	"health-dash/pkg/internal/docs"
	"health-dash/pkg/internal/httpapi"
	"health-dash/pkg/internal/llm"
)

func main() {
	cfg, err := config.Load(".env")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	llmClient := llm.NewClient(
		cfg.LLM.APIKey,
		cfg.LLM.BaseURL,
		cfg.LLM.Model,
		cfg.LLM.RequestTimeout,
	)

	mux := http.NewServeMux()
	api := httpapi.Handler{
		LLM:   llmClient,
		Docs:  docs.NewRegistry(cfg.DataDir),
		Cache: httpapi.NewPageCache(filepath.Join(cfg.DataDir, ".page-cache.json")),
	}
	api.Register(mux)

	server := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      logRequests(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: cfg.LLM.RequestTimeout + 30*time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("backend listening on http://%s", cfg.Addr())
	log.Printf("using model %q at %s", cfg.LLM.Model, cfg.LLM.BaseURL)

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server failed: %v", err)
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
