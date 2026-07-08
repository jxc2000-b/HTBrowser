package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"health-dash/pkg/internal/llm"
	"health-dash/pkg/internal/prompts"
)

const maxUploadBytes = 1 << 20 // 1 MiB of markdown is a very large health log

type uploadRequest struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

// ExtractedClaim is one reading line of the extracted markdown, gate-checked
// against the original upload. "unverified" is not an error: converted or
// repaired values are expected to be absent from the original — that is what
// the user reviews.
type ExtractedClaim struct {
	Line    int    `json:"line"` // 1-indexed line in the extracted markdown
	Metric  string `json:"metric"`
	Date    string `json:"date"`
	Value   string `json:"value"`
	Unit    string `json:"unit,omitempty"`
	Verdict string `json:"verdict"` // "match" or "unverified"
}

type uploadResponse struct {
	Original  string           `json:"original"`
	Extracted string           `json:"extracted"`
	Claims    []ExtractedClaim `json:"claims"`
}

// upload archives the raw document, runs the extractor LLM, and gate-checks
// every extracted value against the original for the review screen. Nothing
// is written to the document bank until the user approves via ingest.
func (h Handler) upload(w http.ResponseWriter, r *http.Request) {
	var req uploadRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxUploadBytes)).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		http.Error(w, "uploaded document is empty", http.StatusBadRequest)
		return
	}

	if err := h.archiveRaw(req.Filename, req.Content); err != nil {
		http.Error(w, "failed to archive original: "+err.Error(), http.StatusInternalServerError)
		return
	}

	system, user := prompts.ExtractorMessages(req.Filename, req.Content)
	extracted, err := h.LLM.Chat(r.Context(), []llm.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	})
	if err != nil {
		http.Error(w, "extraction failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	extracted = strings.TrimSpace(stripFences(extracted))

	writeJSON(w, uploadResponse{
		Original:  req.Content,
		Extracted: extracted,
		Claims:    gateCheckExtracted(extracted, req.Content),
	})
}

type ingestRequest struct {
	// Extracted is the (possibly user-edited) canonical markdown. User edits
	// are terminal: whatever is approved here becomes ground truth.
	Extracted string `json:"extracted"`
}

type ingestResponse struct {
	Files []string `json:"files"` // document IDs written or appended to
}

// ingest deterministically chunks approved canonical markdown into one file
// per metric in the data directory: appended if the metric file exists,
// created otherwise. No merge semantics yet — append only.
func (h Handler) ingest(w http.ResponseWriter, r *http.Request) {
	var req ingestRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxUploadBytes)).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	chunks := parseCanonical(req.Extracted)
	if len(chunks) == 0 {
		http.Error(w, "no metric sections found: expected '# METRIC' headings", http.StatusBadRequest)
		return
	}

	var files []string
	for _, chunk := range chunks {
		id := metricSlug(chunk.Metric)
		if id == "" {
			continue
		}
		path := filepath.Join(h.Docs.Dir, id+".md")
		if err := appendChunk(path, chunk); err != nil {
			http.Error(w, fmt.Sprintf("write %s: %v", id, err), http.StatusInternalServerError)
			return
		}
		files = append(files, id)
	}

	writeJSON(w, ingestResponse{Files: files})
}

// metricChunk is one "# METRIC" section of canonical markdown.
type metricChunk struct {
	Metric   string
	Range    string   // the "range:" line, if present
	Readings []string // reading lines, verbatim
}

// parseCanonical splits canonical measurement markdown into metric chunks.
// Unrecognized lines inside a section are kept as readings — the user
// approved them, and dropping approved content silently would be worse.
func parseCanonical(markdown string) []metricChunk {
	var chunks []metricChunk
	var current *metricChunk
	for _, line := range strings.Split(markdown, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "# "):
			chunks = append(chunks, metricChunk{Metric: strings.TrimSpace(strings.TrimPrefix(line, "# "))})
			current = &chunks[len(chunks)-1]
		case current == nil || line == "":
			continue
		case strings.HasPrefix(strings.ToLower(line), "range:") && current.Range == "":
			current.Range = line
		default:
			current.Readings = append(current.Readings, line)
		}
	}
	return chunks
}

// metricSlug canonicalizes a metric name into a document ID. Identity is
// case- and whitespace-insensitive: "LDL CALCULATED" and "ldl  calculated"
// land in the same file. Distinct names stay distinct (no aliasing).
func metricSlug(metric string) string {
	slug := strings.ToLower(strings.TrimSpace(metric))
	slug = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(slug, "_")
	slug = strings.Trim(slug, "_")
	if !validDocIDString(slug) {
		return ""
	}
	return slug
}

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func validDocIDString(id string) bool {
	return slugPattern.MatchString(id)
}

// appendChunk writes a metric chunk to its file: full section (heading,
// range, readings) when creating, readings only when appending.
func appendChunk(path string, chunk metricChunk) error {
	if len(chunk.Readings) == 0 && chunk.Range == "" {
		return nil
	}

	_, statErr := os.Stat(path)
	exists := statErr == nil

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	var sb strings.Builder
	if !exists {
		sb.WriteString("# " + chunk.Metric + "\n")
		if chunk.Range != "" {
			sb.WriteString(chunk.Range + "\n")
		}
	}
	for _, reading := range chunk.Readings {
		sb.WriteString(reading + "\n")
	}
	_, err = file.WriteString(sb.String())
	return err
}

// archiveRaw stores the untouched upload under DATA_DIR/raw for provenance,
// suffixing a timestamp on name collisions. The registry ignores
// subdirectories, so archived files never appear as documents.
func (h Handler) archiveRaw(filename, content string) error {
	dir := filepath.Join(h.Docs.Dir, "raw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	base := strings.TrimSuffix(filepath.Base(filename), ".md")
	if slug := metricSlug(base); slug != "" {
		base = slug
	} else {
		base = "upload"
	}

	path := filepath.Join(dir, base+".md")
	if _, err := os.Stat(path); err == nil {
		path = filepath.Join(dir, fmt.Sprintf("%s-%s.md", base, time.Now().Format("20060102-150405")))
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// readingPattern matches a canonical reading line: ISO date, value, optional unit.
var readingPattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\s+(\d[\d,]*(?:\.\d+)?|\.\d+)\s*(.*)$`)

// gateCheckExtracted runs the deterministic presence gate over every reading
// line of the extracted markdown: values found verbatim in the original are
// "match"; the rest (conversions, repairs, fabrications alike) are
// "unverified" and highlighted for the user's review. Deliberately no LLM
// pass here — the human is the judge on this screen.
func gateCheckExtracted(extracted, original string) []ExtractedClaim {
	docNumbers := numberTokens(original)
	docLower := strings.ToLower(original)

	var claims []ExtractedClaim
	metric := ""
	for i, line := range strings.Split(extracted, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			metric = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			continue
		}
		match := readingPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		claim := ExtractedClaim{
			Line:    i + 1,
			Metric:  metric,
			Date:    match[1],
			Value:   match[2],
			Unit:    strings.TrimSpace(match[3]),
			Verdict: "unverified",
		}
		if valuePresent(claim.Value, docNumbers, docLower) {
			claim.Verdict = "match"
		}
		claims = append(claims, claim)
	}
	return claims
}

// stripFences removes a markdown code fence the extractor may have wrapped
// its output in despite instructions.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	s = regexp.MustCompile("^```[a-zA-Z]*\n").ReplaceAllString(s, "")
	return strings.TrimSuffix(s, "```")
}
