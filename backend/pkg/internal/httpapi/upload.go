package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
	// Assignments maps a heading's metric name to the user's identity
	// resolution from the review screen: an existing document ID to append
	// to, or "" to force a new standalone file even if the slug collides.
	// Headings absent from the map use the default slug behavior.
	Assignments map[string]string `json:"assignments"`
}

type ingestResponse struct {
	Files []string `json:"files"` // document IDs written or merged into
}

// UnitConflict reports an incoming reading whose unit contradicts the target
// file's established unit. The line/text reference the submitted extracted
// markdown so the review screen can underline the offending unit in place.
type UnitConflict struct {
	Metric       string `json:"metric"`
	Line         int    `json:"line"`
	Text         string `json:"text"`
	Unit         string `json:"unit"`
	ExistingUnit string `json:"existingUnit"`
	Target       string `json:"target"`
}

type ingestConflictResponse struct {
	Error     string         `json:"error"` // "unit_mismatch"
	Conflicts []UnitConflict `json:"conflicts"`
}

// ingest deterministically merges approved canonical markdown into the data
// directory, one file per metric: parse both sides, unite, stable-sort by
// date, dedupe semantic duplicates, rewrite the whole file. A unit mismatch
// against an existing file rejects the entire ingest (nothing is written)
// with a 409 carrying the conflicting lines, so the review screen can send
// the user back to fix them. Merging never converts units and never drops
// content it does not understand.
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

	// Resolve every chunk's target and collect unit conflicts before writing
	// anything: the ingest is all-or-nothing.
	type plannedWrite struct {
		id    string
		chunk metricChunk
	}
	var plan []plannedWrite
	var conflicts []UnitConflict
	for _, chunk := range chunks {
		id := metricSlug(chunk.Metric)
		if id == "" {
			continue
		}
		if target, resolved := req.Assignments[chunk.Metric]; resolved {
			if target != "" && validDocIDString(target) {
				id = target // user matched this heading to an existing document
			} else {
				// User rejected all matches: force a standalone file, suffixing
				// if the slug would collide with an existing document.
				id = uniqueDocID(h.Docs.Dir, id)
			}
		}
		existing := readFileIfExists(filepath.Join(h.Docs.Dir, id+".md"))
		conflicts = append(conflicts, unitConflicts(existing, chunk, id)...)
		plan = append(plan, plannedWrite{id: id, chunk: chunk})
	}

	if len(conflicts) > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(ingestConflictResponse{Error: "unit_mismatch", Conflicts: conflicts})
		return
	}

	var files []string
	for _, write := range plan {
		path := filepath.Join(h.Docs.Dir, write.id+".md")
		// Read fresh: two chunks in one ingest may target the same file.
		merged := mergeCanonical(readFileIfExists(path), write.chunk)
		if err := os.WriteFile(path, []byte(merged), 0o644); err != nil {
			http.Error(w, fmt.Sprintf("write %s: %v", write.id, err), http.StatusInternalServerError)
			return
		}
		files = append(files, write.id)
	}

	writeJSON(w, ingestResponse{Files: files})
}

// reading is one parsed canonical reading line.
type reading struct {
	Line  int // 1-indexed line in the source it was parsed from
	Text  string
	Date  string
	Value string
	Unit  string
}

// metricChunk is one "# METRIC" section of canonical markdown. Lines that
// parse as neither range nor reading are residue: preserved verbatim, never
// merged or deduped — user content is not dropped for being unrecognized.
type metricChunk struct {
	Metric   string
	Range    string
	Readings []reading
	Residue  []string
}

// parseCanonical splits canonical measurement markdown into metric chunks.
func parseCanonical(markdown string) []metricChunk {
	var chunks []metricChunk
	var current *metricChunk
	for i, line := range strings.Split(markdown, "\n") {
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
			if match := readingPattern.FindStringSubmatch(line); match != nil {
				current.Readings = append(current.Readings, reading{
					Line:  i + 1,
					Text:  line,
					Date:  match[1],
					Value: match[2],
					Unit:  strings.TrimSpace(match[3]),
				})
			} else {
				current.Residue = append(current.Residue, line)
			}
		}
	}
	return chunks
}

// parseFilePool parses an existing metric file into a single pool. Files
// should hold one metric, but a hand-edited file with several headings is
// tolerated: the first chunk names the file, all readings merge into one pool.
func parseFilePool(content string) metricChunk {
	chunks := parseCanonical(content)
	if len(chunks) == 0 {
		return metricChunk{}
	}
	pool := chunks[0]
	for _, extra := range chunks[1:] {
		pool.Readings = append(pool.Readings, extra.Readings...)
		pool.Residue = append(pool.Residue, extra.Residue...)
		if pool.Range == "" {
			pool.Range = extra.Range
		}
	}
	return pool
}

// dominantUnit returns the first non-empty normalized unit in a pool — the
// unit the file is established in.
func dominantUnit(pool metricChunk) string {
	for _, r := range pool.Readings {
		if unit := normalizeUnit(r.Unit); unit != "" {
			return unit
		}
	}
	return ""
}

// unitConflicts reports incoming readings whose unit contradicts the target
// file's established unit. Unitless readings never conflict (the extractor
// sometimes drops a unit the source implied); merging never converts.
func unitConflicts(existing string, chunk metricChunk, target string) []UnitConflict {
	existingUnit := dominantUnit(parseFilePool(existing))
	if existingUnit == "" {
		return nil
	}
	var conflicts []UnitConflict
	for _, r := range chunk.Readings {
		if unit := normalizeUnit(r.Unit); unit != "" && unit != existingUnit {
			conflicts = append(conflicts, UnitConflict{
				Metric:       chunk.Metric,
				Line:         r.Line,
				Text:         r.Text,
				Unit:         r.Unit,
				ExistingUnit: existingUnit,
				Target:       target,
			})
		}
	}
	return conflicts
}

// mergeCanonical merges an incoming chunk into an existing file's content and
// returns the full new file: existing heading and range win, readings are
// united, stable-sorted by ISO date (existing before incoming on ties), and
// semantic duplicates (same date, numerically equal value, compatible unit)
// collapse to the existing representation. Residue lines survive at the end.
// The merge is idempotent: re-ingesting the same document changes nothing.
func mergeCanonical(existing string, incoming metricChunk) string {
	pool := parseFilePool(existing)
	if pool.Metric == "" {
		pool.Metric = incoming.Metric
	}
	if pool.Range == "" {
		pool.Range = incoming.Range
	}

	for _, candidate := range incoming.Readings {
		duplicate := false
		for _, kept := range pool.Readings {
			if sameReading(kept, candidate) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			pool.Readings = append(pool.Readings, candidate)
		}
	}
	sort.SliceStable(pool.Readings, func(i, j int) bool {
		return pool.Readings[i].Date < pool.Readings[j].Date // ISO dates sort lexicographically
	})

	for _, line := range incoming.Residue {
		if !containsString(pool.Residue, line) {
			pool.Residue = append(pool.Residue, line)
		}
	}

	var sb strings.Builder
	sb.WriteString("# " + pool.Metric + "\n")
	if pool.Range != "" {
		sb.WriteString(pool.Range + "\n")
	}
	for _, r := range pool.Readings {
		sb.WriteString(r.Text + "\n")
	}
	for _, line := range pool.Residue {
		sb.WriteString(line + "\n")
	}
	return sb.String()
}

// sameReading reports whether two readings are semantic duplicates: same
// date, numerically equal value (74 == 74.0), and compatible units (equal, or
// one side unitless).
func sameReading(a, b reading) bool {
	if a.Date != b.Date {
		return false
	}
	av, aerr := parseValue(a.Value)
	bv, berr := parseValue(b.Value)
	if aerr != nil || berr != nil || av != bv {
		return false
	}
	au, bu := normalizeUnit(a.Unit), normalizeUnit(b.Unit)
	return au == bu || au == "" || bu == ""
}

func parseValue(value string) (float64, error) {
	return strconv.ParseFloat(strings.ReplaceAll(value, ",", ""), 64)
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func readFileIfExists(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(content)
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

// uniqueDocID returns id if no document with that ID exists, otherwise the
// first free numbered variant (id_2, id_3, ...).
func uniqueDocID(dir, id string) string {
	if _, err := os.Stat(filepath.Join(dir, id+".md")); err != nil {
		return id
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s_%d", id, n)
		if _, err := os.Stat(filepath.Join(dir, candidate+".md")); err != nil {
			return candidate
		}
	}
}

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func validDocIDString(id string) bool {
	return slugPattern.MatchString(id)
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
