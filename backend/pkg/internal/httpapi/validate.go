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
	Verdict string `json:"verdict"` // "match", "no_match", "citation_mismatch"
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

	// Layer 2: LLM binds each surviving claim to a source line.
	verdicts, err := h.runValidatorLLM(r, markdown, llmClaims)
	if err != nil {
		log.Printf("validator LLM failed: %v", err)
		http.Error(w, "validator failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Layer 3: machine-check the citations. A "match" only stands if the
	// cited line actually contains the claimed value.
	for i := range results {
		verdict, ok := verdicts[results[i].ID]
		if !ok {
			continue // gate-failed or missing from LLM output; stays no_match
		}
		results[i].Line = verdict.Line
		results[i].Note = verdict.Note
		if verdict.Verdict != "match" {
			results[i].Verdict = "no_match"
			continue
		}
		if verdict.Line < 1 || verdict.Line > len(lines) {
			results[i].Verdict = "citation_mismatch"
			results[i].Note = "cited line does not exist"
			continue
		}
		cited := lines[verdict.Line-1]
		if !valuePresent(results[i].Value, numberTokens(cited), strings.ToLower(cited)) {
			results[i].Verdict = "citation_mismatch"
			results[i].Note = fmt.Sprintf("cited line %d does not contain the value", verdict.Line)
			continue
		}
		results[i].Verdict = "match"
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
	spanClaimPattern = regexp.MustCompile(`(?i)<span\b[^>]*\bdata-value\s*=\s*"[^"]*"[^>]*>`)
	// Matches data-chart='...' or data-chart="..." in document order so
	// ChartIndex lines up with the DOM's canvas[data-chart] order.
	canvasChartPattern = regexp.MustCompile(`(?i)<canvas\b[^>]*\bdata-chart\s*=\s*(?:'([^']*)'|"([^"]*)")`)
	numberPattern      = regexp.MustCompile(`-?(?:\d[\d,]*(?:\.\d+)?|\.\d+)`)
	// Models sometimes emit ".9" instead of "0.9", which is invalid JSON.
	bareDecimalPattern = regexp.MustCompile(`([\[,:\s])\.(\d)`)
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
	pattern := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\s*=\s*"([^"]*)"`)
	if match := pattern.FindStringSubmatch(tag); match != nil {
		return html.UnescapeString(match[1])
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

func numberTokens(text string) []float64 {
	var numbers []float64
	for _, token := range numberPattern.FindAllString(text, -1) {
		token = strings.ReplaceAll(token, ",", "")
		if value, err := strconv.ParseFloat(token, 64); err == nil {
			numbers = append(numbers, value)
		}
	}
	return numbers
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("write json response: %v", err)
	}
}
