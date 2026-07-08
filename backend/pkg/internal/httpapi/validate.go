package httpapi

import (
	"encoding/json"
	"fmt"
	"html"
	"log"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"health-dash/pkg/internal/llm"
	"health-dash/pkg/internal/prompts"
)

type validateRequest struct {
	Doc  string `json:"doc"`
	HTML string `json:"html"`
}

// Claim is one displayed value extracted from the generated page.
// Kind "span" claims map to span[data-value] elements in DOM order via
// SpanIndex; kind "chart" claims map to canvas[data-chart] elements via
// ChartIndex (one claim per plotted value).
type Claim struct {
	ID         int    `json:"id"`
	Kind       string `json:"kind"`
	SpanIndex  int    `json:"spanIndex,omitempty"`
	ChartIndex int    `json:"chartIndex,omitempty"`
	Value      string `json:"value"`
	Label      string `json:"label,omitempty"`
	Unit       string `json:"unit,omitempty"`
	Date       string `json:"date,omitempty"`
}

type ClaimResult struct {
	Claim
	Line    int    `json:"line"`
	Verdict string `json:"verdict"` // "match", "no_match", "citation_mismatch", "unverified"
	Note    string `json:"note,omitempty"`
}

type validateResponse struct {
	Status string        `json:"status"` // "verified", "partial", "no_claims"
	Claims []ClaimResult `json:"claims"`
}

func (h Handler) validate(w http.ResponseWriter, r *http.Request) {
	var req validateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	markdown, err := h.Docs.Read(req.Doc)
	if err != nil {
		http.Error(w, "unknown document: "+req.Doc, http.StatusNotFound)
		return
	}

	claims := extractClaims(req.HTML)
	if len(claims) == 0 {
		writeJSON(w, validateResponse{Status: "no_claims", Claims: []ClaimResult{}})
		return
	}

	docNumbers := numberTokens(markdown)
	docLower := strings.ToLower(markdown)
	docUnits := normalizeUnit(markdown)
	lines := strings.Split(markdown, "\n")

	results := make([]ClaimResult, len(claims))
	var llmClaims []Claim

	// Layer 1: deterministic presence gate. A value that appears nowhere in
	// the document is a guaranteed fabrication — no LLM needed to reject it.
	for i, claim := range claims {
		results[i] = ClaimResult{Claim: claim, Verdict: "no_match"}
		if claim.Kind == "chart" && claim.Value == "" {
			results[i].Note = "chart data attribute is not valid JSON; chart could not be validated"
			continue
		}
		if valuePresent(claim.Value, docNumbers, docLower) {
			llmClaims = append(llmClaims, claim)
		} else {
			results[i].Note = "value not found anywhere in document"
		}
	}

	// Layer 2: LLM binds each surviving claim to a source line. A validator
	// outage must not take down an otherwise-rendered page: retry once, then
	// degrade to "unverified" (dashed, not flagged red) for the claims that
	// passed the deterministic gate.
	verdicts, err := h.runValidatorLLM(r, markdown, llmClaims)
	if err != nil {
		log.Printf("validator LLM failed (retrying): %v", err)
		verdicts, err = h.runValidatorLLM(r, markdown, llmClaims)
	}
	if err != nil {
		log.Printf("validator LLM failed twice, degrading to unverified: %v", err)
		for _, claim := range llmClaims {
			for i := range results {
				if results[i].ID == claim.ID {
					results[i].Verdict = "unverified"
					results[i].Note = "validator unavailable; value passed the presence check but was not verified in context"
				}
			}
		}
		writeJSON(w, validateResponse{Status: "partial", Claims: results})
		return
	}

	// Layer 3: machine-check the citations. The LLM's job is the semantic
	// judgment (is this value used in the right context); the machine confirms
	// the value really sits on a real line. We do not fail a claim just because
	// the LLM cited the wrong line number — if the value is locatable on any
	// line we accept it and correct the citation. The unique thing the LLM
	// guards is a "no_match" verdict (right number, wrong metric/date).
	for i := range results {
		verdict, ok := verdicts[results[i].ID]
		if !ok {
			continue // gate-failed or missing from LLM output; stays no_match
		}
		results[i].Note = verdict.Note
		if verdict.Verdict != "match" {
			results[i].Verdict = "no_match"
			continue
		}

		// Hallucinated unit: claimed an alphabetic unit the document never uses
		// anywhere (e.g. inventing mg/dL where the source has none). Symbol-only
		// units like "%" are skipped here — they are often implied by the source
		// ("…_pct") and left to the LLM's semantic unit judgment.
		if unit := normalizeUnit(results[i].Unit); unit != "" && hasLetter(unit) && !containsUnit(docUnits, unit) {
			results[i].Verdict = "no_match"
			results[i].Line = 0
			results[i].Note = fmt.Sprintf("unit %q does not appear anywhere in the document", results[i].Unit)
			continue
		}

		line := locateValueLine(results[i].Claim, lines, verdict.Line)
		if line == 0 {
			results[i].Verdict = "citation_mismatch"
			results[i].Line = 0
			results[i].Note = "value could not be located on any line of the document"
			continue
		}
		results[i].Line = line
		results[i].Verdict = "match"
		results[i].Note = ""
	}

	status := "verified"
	for _, result := range results {
		if result.Verdict != "match" {
			status = "partial"
			break
		}
	}

	writeJSON(w, validateResponse{Status: status, Claims: results})
}

type llmVerdict struct {
	ID      int    `json:"id"`
	Line    int    `json:"line"`
	Verdict string `json:"verdict"`
	Note    string `json:"note"`
}

func (h Handler) runValidatorLLM(r *http.Request, markdown string, claims []Claim) (map[int]llmVerdict, error) {
	verdicts := make(map[int]llmVerdict)
	if len(claims) == 0 {
		return verdicts, nil
	}

	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return nil, err
	}

	system, user := prompts.ValidatorMessages(markdown, string(claimsJSON))
	response, err := h.LLM.Chat(r.Context(), []llm.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	})
	if err != nil {
		return nil, err
	}

	parsed, err := parseVerdicts(response)
	if err != nil {
		return nil, err
	}
	for _, verdict := range parsed {
		verdicts[verdict.ID] = verdict
	}
	return verdicts, nil
}

// parseVerdicts tolerates markdown fences and surrounding prose around the
// JSON array the validator was instructed to return.
func parseVerdicts(response string) ([]llmVerdict, error) {
	start := strings.Index(response, "[")
	end := strings.LastIndex(response, "]")
	if start == -1 || end <= start {
		return nil, fmt.Errorf("validator response contains no JSON array")
	}
	var verdicts []llmVerdict
	if err := json.Unmarshal([]byte(response[start:end+1]), &verdicts); err != nil {
		return nil, fmt.Errorf("parse validator response: %w", err)
	}
	return verdicts, nil
}

var (
	// Both quote styles are accepted so the extracted claims stay index-aligned
	// with the frontend's querySelectorAll('span[data-value]'), which matches
	// single-quoted attributes too.
	spanClaimPattern = regexp.MustCompile(`(?i)<span\b[^>]*\bdata-value\s*=\s*(?:"[^"]*"|'[^']*')[^>]*>`)
	// Matches data-chart='...' or data-chart="..." in document order so
	// ChartIndex lines up with the DOM's canvas[data-chart] order.
	canvasChartPattern = regexp.MustCompile(`(?i)<canvas\b[^>]*\bdata-chart\s*=\s*(?:'([^']*)'|"([^"]*)")`)
	// No leading minus: a hyphen between digits is a range separator ("9-23"
	// means 9 and 23), not a negative sign. Health values are not negative.
	numberPattern = regexp.MustCompile(`\d[\d,]*(?:\.\d+)?|\.\d+`)
	// Models sometimes emit ".9" instead of "0.9", which is invalid JSON.
	bareDecimalPattern = regexp.MustCompile(`([\[,:\s])\.(\d)`)
	wordNumberPattern  = regexp.MustCompile(`(?i)[a-z]+(?:[ -]+(?:and[ -]+)?[a-z]+)*`)
)

type chartSpec struct {
	Type   string          `json:"type"`
	Title  string          `json:"title"`
	Unit   string          `json:"unit"`
	Labels []string        `json:"labels"`
	Values json.RawMessage `json:"values"`
}

func extractClaims(pageHTML string) []Claim {
	var claims []Claim

	for spanIndex, tag := range spanClaimPattern.FindAllString(pageHTML, -1) {
		claims = append(claims, Claim{
			ID:        len(claims),
			Kind:      "span",
			SpanIndex: spanIndex,
			Value:     attrValue(tag, "data-value"),
			Label:     attrValue(tag, "data-label"),
			Unit:      attrValue(tag, "data-unit"),
			Date:      attrValue(tag, "data-date"),
		})
	}

	for chartIndex, match := range canvasChartPattern.FindAllStringSubmatch(pageHTML, -1) {
		rawAttr := match[1]
		if rawAttr == "" {
			rawAttr = match[2]
		}
		rawJSON := bareDecimalPattern.ReplaceAllString(html.UnescapeString(rawAttr), "${1}0.$2")

		spec, values, err := parseChartSpec(rawJSON)
		if err != nil {
			// An unparseable chart must fail loudly, not skip validation:
			// a Value-less chart claim is force-failed by the handler.
			claims = append(claims, Claim{
				ID:         len(claims),
				Kind:       "chart",
				ChartIndex: chartIndex,
				Label:      "unparseable chart",
			})
			continue
		}
		for _, value := range values {
			claims = append(claims, Claim{
				ID:         len(claims),
				Kind:       "chart",
				ChartIndex: chartIndex,
				Value:      value.value,
				Label:      spec.Title,
				Unit:       spec.Unit,
				Date:       value.date,
			})
		}
	}

	return claims
}

type chartValue struct {
	value string
	date  string
}

// parseChartSpec decodes a data-chart JSON payload. Values are kept as their
// raw tokens ("5.10" stays "5.10", not 5.1) so the strict validator is not
// fed a reformatted number. Both flat arrays and (against instructions but
// tolerated) nested per-series arrays are handled; nulls are gaps.
func parseChartSpec(rawJSON string) (chartSpec, []chartValue, error) {
	var spec chartSpec
	if err := json.Unmarshal([]byte(rawJSON), &spec); err != nil {
		return spec, nil, err
	}

	decoder := json.NewDecoder(strings.NewReader(string(spec.Values)))
	decoder.UseNumber()
	var rawValues []any
	if err := decoder.Decode(&rawValues); err != nil {
		return spec, nil, fmt.Errorf("decode chart values: %w", err)
	}

	dateFor := func(i int) string {
		if i < len(spec.Labels) {
			return spec.Labels[i]
		}
		return ""
	}

	var values []chartValue
	for i, item := range rawValues {
		switch typed := item.(type) {
		case json.Number:
			values = append(values, chartValue{value: typed.String(), date: dateFor(i)})
		case nil:
			continue
		case []any:
			for j, inner := range typed {
				if number, ok := inner.(json.Number); ok {
					values = append(values, chartValue{value: number.String(), date: dateFor(j)})
				}
			}
		}
	}
	return spec, values, nil
}

func attrValue(tag, name string) string {
	pattern := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	if match := pattern.FindStringSubmatch(tag); match != nil {
		value := match[1]
		if value == "" {
			value = match[2]
		}
		return html.UnescapeString(value)
	}
	return ""
}

// valuePresent reports whether a claimed value exists in the given text. All
// numeric tokens in the claim must be present as numbers (so "0.4-4.0" needs
// both); claims without numbers fall back to a case-insensitive substring test.
func valuePresent(value string, textNumbers []float64, textLower string) bool {
	claimNumbers := numberTokens(value)
	if len(claimNumbers) == 0 {
		return strings.Contains(textLower, strings.ToLower(strings.TrimSpace(value)))
	}
	for _, claimed := range claimNumbers {
		found := false
		for _, present := range textNumbers {
			if math.Abs(claimed-present) < 1e-9 {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// locateValueLine finds the best line containing the claim's value, preferring
// lines that also carry the claim's date/label context. hint is the LLM's cited
// line, used only as a tie-breaker. Returns 0 if no line contains the value.
func locateValueLine(claim Claim, lines []string, hint int) int {
	best, bestScore := 0, -1
	for i, line := range lines {
		if !valuePresent(claim.Value, numberTokens(line), strings.ToLower(line)) {
			continue
		}
		score := 0
		lower := strings.ToLower(line)
		if claim.Date != "" && strings.Contains(lower, strings.ToLower(claim.Date)) {
			score += 2
		}
		if claim.Label != "" {
			for _, word := range strings.Fields(strings.ToLower(claim.Label)) {
				if len(word) > 2 && strings.Contains(lower, word) {
					score++
				}
			}
		}
		if i+1 == hint {
			score++ // agree with the LLM when otherwise tied
		}
		if score > bestScore {
			best, bestScore = i+1, score
		}
	}
	return best
}

// normalizeUnit lowercases and strips whitespace so unit strings compare
// independent of spacing ("mg/dL" vs "mg / dl").
func normalizeUnit(unit string) string {
	return strings.ToLower(strings.Join(strings.Fields(unit), ""))
}

// containsUnit reports whether unit occurs in the normalized document text at
// a letter boundary. Because normalization strips whitespace, a plain
// substring search can match across word seams (unit "gm" inside "…mg ml…" →
// "mgml"); requiring non-letter neighbours prevents that.
func containsUnit(docUnits, unit string) bool {
	for from := 0; ; {
		i := strings.Index(docUnits[from:], unit)
		if i == -1 {
			return false
		}
		i += from
		beforeOK := i == 0 || !isLetterByte(docUnits[i-1])
		after := i + len(unit)
		afterOK := after == len(docUnits) || !isLetterByte(docUnits[after])
		if beforeOK && afterOK {
			return true
		}
		from = i + 1
	}
}

func isLetterByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b >= 0x80
}

func hasLetter(s string) bool {
	return strings.IndexFunc(s, unicode.IsLetter) >= 0
}

func numberTokens(text string) []float64 {
	var numbers []float64
	for _, token := range numberPattern.FindAllString(text, -1) {
		token = strings.ReplaceAll(token, ",", "")
		if value, err := strconv.ParseFloat(token, 64); err == nil {
			numbers = append(numbers, value)
		}
	}
	// Also surface numbers the document spells out in words ("twenty-eight"),
	// so a digit claim (28) can be verified against a worded source.
	return append(numbers, spelledNumbers(text)...)
}

var numberWords = map[string]int{
	"zero": 0, "one": 1, "two": 2, "three": 3, "four": 4, "five": 5,
	"six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10, "eleven": 11,
	"twelve": 12, "thirteen": 13, "fourteen": 14, "fifteen": 15, "sixteen": 16,
	"seventeen": 17, "eighteen": 18, "nineteen": 19, "twenty": 20, "thirty": 30,
	"forty": 40, "fifty": 50, "sixty": 60, "seventy": 70, "eighty": 80, "ninety": 90,
}

var tensWords = []string{"twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"}

// spelledNumbers extracts numbers written as words. It handles standard forms
// ("eighty-four" → 84, "one hundred and eighty" → 180) and the concatenated
// form some sources use ("twentyeight" → 28). It is intentionally bounded to
// values under a few thousand — enough for health figures, not a full parser.
func spelledNumbers(text string) []float64 {
	var results []float64
	for _, phrase := range wordNumberPattern.FindAllString(strings.ToLower(text), -1) {
		fields := strings.FieldsFunc(phrase, func(r rune) bool { return r == ' ' || r == '-' })

		var tokens []string
		for _, field := range fields {
			if field == "and" || field == "" {
				continue
			}
			tokens = append(tokens, splitConcatenated(field)...)
		}

		current, total := 0, 0
		any, sawScale := false, false
		flush := func() {
			if any && (sawScale || total+current > 0) {
				results = append(results, float64(total+current))
			}
			current, total, any, sawScale = 0, 0, false, false
		}
		for _, token := range tokens {
			switch {
			case token == "hundred":
				if current == 0 {
					current = 1
				}
				current *= 100
				any, sawScale = true, true
			case token == "thousand":
				if current == 0 {
					current = 1
				}
				total += current * 1000
				current = 0
				any, sawScale = true, true
			default:
				if value, ok := numberWords[token]; ok {
					current += value
					any = true
				} else {
					flush()
				}
			}
		}
		flush()
	}
	return results
}

// splitConcatenated splits a glued tens+ones word like "twentyeight" into
// ["twenty", "eight"]; otherwise returns the word unchanged.
func splitConcatenated(word string) []string {
	if _, ok := numberWords[word]; ok {
		return []string{word}
	}
	for _, tens := range tensWords {
		if strings.HasPrefix(word, tens) {
			rest := word[len(tens):]
			if value, ok := numberWords[rest]; ok && value < 10 {
				return []string{tens, rest}
			}
		}
	}
	return []string{word}
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("write json response: %v", err)
	}
}
