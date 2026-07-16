package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"health-dash/pkg/internal/docs"
	"health-dash/pkg/internal/llm"
	"health-dash/pkg/internal/prompts"
)

type Handler struct {
	LLM   *llm.Client
	Docs  docs.Registry
	Cache *PageCache
}

func (h Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", h.health)
	mux.HandleFunc("GET /api/docs", h.listDocs)
	mux.HandleFunc("POST /api/generate", h.generate)
	mux.HandleFunc("POST /api/validate", h.validate)
	mux.HandleFunc("POST /api/upload", h.upload)
	mux.HandleFunc("POST /api/ingest", h.ingest)
}

// listDocs returns the document registry as plain JSON so the frontend can
// render navigation instantly, without waiting for an LLM-generated homepage.
func (h Handler) listDocs(w http.ResponseWriter, r *http.Request) {
	registry, err := h.Docs.List()
	if err != nil {
		http.Error(w, "failed to list documents: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if registry == nil {
		registry = []docs.DocInfo{}
	}
	writeJSON(w, registry)
}

func (h Handler) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

type generateRequest struct {
	// Doc is the document ID to render; empty means the homepage.
	Doc string `json:"doc"`
	// RepairNotes lists values that failed validation on a previous attempt.
	RepairNotes []string `json:"repairNotes"`
	// Force bypasses the homepage cache (explicit user regeneration).
	Force bool `json:"force"`
}

func (h Handler) generate(w http.ResponseWriter, r *http.Request) {
	if h.LLM == nil {
		http.Error(w, "LLM client is not configured", http.StatusInternalServerError)
		return
	}

	var req generateRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req) // empty body means homepage
	}

	// The homepage is pure registry data — rendered programmatically, no LLM,
	// no cache.
	if req.Doc == "" {
		registry, err := h.Docs.List()
		if err != nil {
			http.Error(w, "failed to list documents: "+err.Error(), http.StatusInternalServerError)
			return
		}
		streamCached(w, homePage(registry))
		return
	}

	// "overview" is a reserved page: every chartable metric across all
	// canonical documents, rendered programmatically. It shadows any real
	// document that happens to be named overview.md.
	if req.Doc == "overview" {
		registry, err := h.Docs.List()
		if err != nil {
			http.Error(w, "failed to list documents: "+err.Error(), http.StatusInternalServerError)
			return
		}
		streamCached(w, overviewPage(h.Docs, registry))
		return
	}

	markdown, err := h.Docs.Read(req.Doc)
	if err != nil {
		http.Error(w, "unknown document: "+req.Doc, http.StatusNotFound)
		return
	}
	info := docInfoFor(h.Docs, req.Doc)
	// Canonical documents render programmatically too: the file IS the chart
	// spec, so no LLM, no cache, and nothing to hallucinate.
	if page, ok := canonicalPage(info, markdown); ok {
		streamCached(w, page)
		return
	}
	// Legacy non-canonical documents fall back to LLM generation.
	system, user := prompts.DocMessages(info, markdown, req.RepairNotes)
	// Hash the repair-free prompt so a repair pass saves under the same key a
	// normal visit will look up.
	baseSystem, baseUser := prompts.DocMessages(info, markdown, nil)
	cacheKey, cacheHash := "doc:"+req.Doc, contentHash(baseSystem+baseUser)

	// A repair pass must never read the cache: the cached page is the one that
	// just failed validation. Its (better) output still overwrites the cache.
	if !req.Force && len(req.RepairNotes) == 0 {
		if cached, ok := h.Cache.Load(cacheKey, cacheHash); ok {
			streamCached(w, cached)
			return
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if err := writeSSE(w, "start", map[string]string{"status": "generating"}); err != nil {
		log.Printf("write start event: %v", err)
		return
	}
	flush(w)

	messages := []llm.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}

	var generated strings.Builder
	err = h.LLM.StreamChat(r.Context(), messages, func(delta string) error {
		generated.WriteString(delta)
		if err := writeSSE(w, "delta", map[string]string{"text": delta}); err != nil {
			return err
		}
		flush(w)
		return nil
	})
	if err != nil {
		log.Printf("generate stream failed: %v", err)
		_ = writeSSE(w, "error", map[string]string{"message": err.Error()})
		flush(w)
		return
	}

	h.Cache.Save(cacheKey, cacheHash, generated.String())

	if err := writeSSE(w, "done", map[string]string{"status": "done"}); err != nil {
		log.Printf("write done event: %v", err)
		return
	}
	flush(w)
}

// streamCached replays a cached page over the same SSE protocol so the
// frontend handles cache hits identically to live generations.
func streamCached(w http.ResponseWriter, html string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	for _, event := range []struct {
		name    string
		payload map[string]string
	}{
		{"start", map[string]string{"status": "cached"}},
		{"delta", map[string]string{"text": html}},
		{"done", map[string]string{"status": "done"}},
	} {
		if err := writeSSE(w, event.name, event.payload); err != nil {
			log.Printf("write cached %s event: %v", event.name, err)
			return
		}
	}
	flush(w)
}

func docInfoFor(registry docs.Registry, id string) docs.DocInfo {
	if infos, err := registry.List(); err == nil {
		for _, info := range infos {
			if info.ID == id {
				return info
			}
		}
	}
	return docs.DocInfo{ID: id, Title: id}
}
