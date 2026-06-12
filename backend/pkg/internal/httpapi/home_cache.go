package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"sync"

	"health-dash/pkg/internal/docs"
)

// HomeCache stores the last generated homepage, keyed by a hash of the
// document registry. The homepage only depends on which documents exist (ids,
// titles, summaries), so it is served from cache until the registry changes
// or the user forces a regeneration.
type HomeCache struct {
	mu   sync.Mutex
	path string
}

type homeCacheEntry struct {
	Hash string `json:"hash"`
	HTML string `json:"html"`
}

func NewHomeCache(path string) *HomeCache {
	return &HomeCache{path: path}
}

func RegistryHash(registry []docs.DocInfo) string {
	payload, err := json.Marshal(registry)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (c *HomeCache) Load(hash string) (string, bool) {
	if c == nil || hash == "" {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	content, err := os.ReadFile(c.path)
	if err != nil {
		return "", false
	}
	var entry homeCacheEntry
	if err := json.Unmarshal(content, &entry); err != nil || entry.Hash != hash || entry.HTML == "" {
		return "", false
	}
	return entry.HTML, true
}

func (c *HomeCache) Save(hash, html string) {
	if c == nil || hash == "" || html == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	payload, err := json.Marshal(homeCacheEntry{Hash: hash, HTML: html})
	if err != nil {
		return
	}
	if err := os.WriteFile(c.path, payload, 0o644); err != nil {
		log.Printf("write home cache: %v", err)
	}
}
