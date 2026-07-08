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

// PageCache stores the last generated page per key ("home", "doc:<id>"), each
// guarded by a content hash: the registry hash for the homepage, the document
// content hash for doc pages. A page is served from cache until its source
// changes or the user forces a regeneration.
type PageCache struct {
	mu   sync.Mutex
	path string
}

type pageCacheEntry struct {
	Hash string `json:"hash"`
	HTML string `json:"html"`
}

func NewPageCache(path string) *PageCache {
	return &PageCache{path: path}
}

func RegistryHash(registry []docs.DocInfo) string {
	payload, err := json.Marshal(registry)
	if err != nil {
		return ""
	}
	return contentHash(string(payload))
}

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func (c *PageCache) Load(key, hash string) (string, bool) {
	if c == nil || key == "" || hash == "" {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.read()[key]
	if !ok || entry.Hash != hash || entry.HTML == "" {
		return "", false
	}
	return entry.HTML, true
}

func (c *PageCache) Save(key, hash, html string) {
	if c == nil || key == "" || hash == "" || html == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	entries := c.read()
	entries[key] = pageCacheEntry{Hash: hash, HTML: html}
	payload, err := json.Marshal(entries)
	if err != nil {
		return
	}
	if err := os.WriteFile(c.path, payload, 0o644); err != nil {
		log.Printf("write page cache: %v", err)
	}
}

// read loads the cache file; callers must hold c.mu. A missing or corrupt file
// (including the pre-PageCache single-entry format) is treated as empty.
func (c *PageCache) read() map[string]pageCacheEntry {
	entries := make(map[string]pageCacheEntry)
	content, err := os.ReadFile(c.path)
	if err != nil {
		return entries
	}
	if err := json.Unmarshal(content, &entries); err != nil {
		return make(map[string]pageCacheEntry)
	}
	return entries
}
