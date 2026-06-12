package docs

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Registry exposes a directory of markdown health documents. Document IDs are
// the file stem (thyroid.md -> "thyroid") and are strictly validated so that
// model-generated hrefs can never read outside the data directory.
type Registry struct {
	Dir string
}

type DocInfo struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

var validDocID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

func NewRegistry(dir string) Registry {
	return Registry{Dir: dir}
}

func (r Registry) List() ([]DocInfo, error) {
	entries, err := os.ReadDir(r.Dir)
	if err != nil {
		return nil, fmt.Errorf("read data directory %q: %w", r.Dir, err)
	}

	var infos []DocInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".md")
		if !validDocID.MatchString(id) {
			continue
		}

		title, summary := describe(filepath.Join(r.Dir, entry.Name()), id)
		infos = append(infos, DocInfo{ID: id, Title: title, Summary: summary})
	}

	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
	return infos, nil
}

// Read returns the full markdown content for a document ID.
func (r Registry) Read(id string) (string, error) {
	if !validDocID.MatchString(id) {
		return "", fmt.Errorf("invalid document id %q", id)
	}
	content, err := os.ReadFile(filepath.Join(r.Dir, id+".md"))
	if err != nil {
		return "", fmt.Errorf("read document %q: %w", id, err)
	}
	return string(content), nil
}

// describe extracts a title (first # heading) and a one-line summary (first
// plain-text paragraph line) without parsing markdown fully.
func describe(path, fallbackTitle string) (string, string) {
	content, err := os.ReadFile(path)
	if err != nil {
		return fallbackTitle, ""
	}

	title := fallbackTitle
	summary := ""
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# ") && title == fallbackTitle {
			title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			continue
		}
		if !strings.HasPrefix(line, "#") && summary == "" {
			summary = line
			if len(summary) > 140 {
				summary = summary[:140] + "…"
			}
		}
		if title != fallbackTitle && summary != "" {
			break
		}
	}
	return title, summary
}
