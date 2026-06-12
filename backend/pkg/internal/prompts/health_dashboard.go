package prompts

import (
	"encoding/json"
	"fmt"
	"strings"

	"health-dash/pkg/internal/docs"
)

// pageContract is shared by the homepage and document pages: output format,
// styling, navigation, and the data-attribution markup the validator depends on.
const pageContract = `You generate complete HTML documents for a local personal health dashboard.

Return only a full HTML document. Do not wrap it in Markdown fences. Do not include commentary before or after the HTML.

STRUCTURE:
Return a full HTML document with a <head> and a <body>:

<html>
<head>
  <title>HealthDash - Page Name</title>
  <meta name="color-scheme" content="light">
  <link href="https://fonts.googleapis.com/css2?family=ChosenFont:wght@400;500;600;700&display=swap" rel="stylesheet">
</head>
<body style="font-family: 'Chosen Font', sans-serif">
  ...page content...
</body>
</html>

Keep the <head> minimal — just the <title>, <meta name="color-scheme">, and a Google Fonts <link>. Tailwind CSS and scripts are injected automatically.
Set color-scheme to "light" or "dark" — choose whichever suits the page. Use only one.

STYLING:
Use Tailwind CSS utility classes for all styling. Create a rich, polished, technical-feeling dashboard.
Use Google Fonts. Include the <link> tag in <head> and apply the font via an inline style on the <body> tag (e.g., style="font-family: 'Inter', sans-serif").
For icons, use Material Symbols: <span class="material-symbols-outlined">icon_name</span> (e.g., monitor_heart, science, bedtime, folder_open, trending_up).
Do not write <script> tags. Do not load external resources other than the one Google Fonts link.

NAVIGATION:
Link to the dashboard home with <a href="/">.
Link to a health document page with <a href="/doc/DOC_ID"> using exactly the document IDs you are given. Never invent document IDs.

SAFETY:
- Do not diagnose.
- Do not recommend treatment.
- Phrase insights as visualization notes, not medical advice.`

const homeInstructions = `TASK:
Generate the dashboard HOME page: a polished navigation hub for the user's health documents.

You are given the registry of available documents as JSON. Create a card or tile per document linking to /doc/DOC_ID, using each document's title and summary. Make the page feel like a local-first technical tool for exploring personal health data. Do not display any specific health values, since you have not been given any document contents — the cards describe and link to the documents only.`

const docInstructions = `TASK:
Generate the dashboard page for ONE health document. The complete markdown source of that document is provided below. It is the ONLY source of truth.

DATA RULES (critical):
- Every health data value you display (numbers, dates, ranges, scores) MUST come verbatim from the document. Never estimate, extrapolate, average, convert units, or invent values.
- Copy values character-for-character as the document formats them: 5.10 stays 5.10 (not 5.1), 0.9 stays 0.9 (not .9).
- Wrap EVERY displayed health value in a claim span:
    <span data-value="4.2" data-label="TSH" data-unit="mIU/L" data-date="2026-03-01">4.2</span>
  data-value is the bare value, data-label names the metric as the document names it, data-unit and data-date are included when the document provides them. The visible text must equal data-value (plus the unit if you show it inline).
- If a value you want is not in the document, render "—" with no claim span instead.
- Reference ranges and qualitative statuses from the document get claim spans too.

CHARTS (critical):
Where the document contains a time series or comparable values, include charts. Do NOT write any JavaScript. Instead emit a canvas element whose data-chart attribute is single-quoted JSON, and the host page renders it with Chart.js:

  <div class="h-64"><canvas data-chart='{"type":"line","title":"TSH over time","unit":"mIU/L","labels":["2025-01-15","2025-06-02"],"values":[4.2,3.8]}'></canvas></div>

- "type" is one of: line, bar, doughnut.
- "labels" and "values" must come verbatim from the document and stay in document order.
- Each chart plots exactly ONE metric series: "values" is a flat array of numbers. Use a separate chart per metric; never nest arrays.
- Write valid JSON numbers with leading zeros (0.9, never .9). Keep the document's formatting: if the document says 5.10, write 5.10, not 5.1.
- Always wrap the canvas in a div with a Tailwind height class (e.g. h-64).
- Include 1-3 charts when the data supports them.

CONTENT:
Build a rich page: headline stats, charts, a detailed table of readings, and a "visualization notes" section. Show the source document name. Link back to home.

VISUALIZATION NOTES:
The notes section must briefly define each variable being measured on the page: what the metric is and what it measures in the body, in one or two plain-language sentences each (e.g. "TSH (thyroid stimulating hormone) is produced by the pituitary gland and signals the thyroid to make hormones; it is commonly used to screen thyroid function."). These are general educational definitions, not interpretations of the user's results. Do not introduce any numeric thresholds or targets in the definitions unless they appear in the document (wrapped in claim spans like any other value).`

const validatorSystemPrompt = `You are a strict data validator for a health dashboard. You verify that values displayed on a generated page exist in a source markdown document.

You are given:
1. The source document with line numbers, formatted as "N| content".
2. A JSON array of claims extracted from the rendered page: {"id", "value", "label", "unit", "date"}.

For each claim, find the line in the document that contains that exact value used in that context (matching label/date where given).

Respond with ONLY a JSON array, no prose, no markdown fences:
[{"id": 0, "line": 12, "verdict": "match"}, {"id": 1, "line": 0, "verdict": "no_match", "note": "value not in document"}]

Rules:
- "match" requires the value to appear on the cited line. Cite the single most relevant line number.
- If the value appears nowhere, or only in a different context (wrong metric, wrong date), the verdict is "no_match" with line 0 and a brief note.
- Be strict: a rounded, converted, or computed value that does not appear in the document is "no_match".
- Pure formatting differences are NOT mismatches: 5.10 in the document matches a claim of 5.1, and 0.9 matches .9 — the number is identical. Only genuine rounding or changed digits (3.93 vs 3.9) fail.
- Never invent line numbers.`

// HomeMessages builds the prompt pair for the homepage.
func HomeMessages(registry []docs.DocInfo) (system string, user string) {
	registryJSON, _ := json.MarshalIndent(registry, "", "  ")
	system = pageContract + "\n\n" + homeInstructions
	user = fmt.Sprintf("Generate the dashboard home page.\n\nDOCUMENT REGISTRY:\n%s", registryJSON)
	return system, user
}

// DocMessages builds the prompt pair for a single document page. repairNotes,
// when non-empty, lists values that failed validation on a previous attempt.
func DocMessages(doc docs.DocInfo, markdown string, repairNotes []string) (system string, user string) {
	system = pageContract + "\n\n" + docInstructions

	var sb strings.Builder
	fmt.Fprintf(&sb, "Generate the dashboard page for document %q (id: %s).\n\nSOURCE DOCUMENT (%s.md):\n<<<DOCUMENT\n%s\nDOCUMENT", doc.Title, doc.ID, doc.ID, markdown)
	sb.WriteString("\n\nRemember: every displayed value needs a claim span, charts use data-chart JSON, and nothing may be displayed that is not verbatim in the document.")

	if len(repairNotes) > 0 {
		sb.WriteString("\n\nIMPORTANT — REPAIR PASS: a previous attempt displayed values that failed validation against the document. Do not display these again unless they appear verbatim in the document:\n")
		for _, note := range repairNotes {
			sb.WriteString("- " + note + "\n")
		}
	}

	return system, sb.String()
}

// ValidatorMessages builds the prompt pair for the validation pass.
func ValidatorMessages(markdown string, claimsJSON string) (system string, user string) {
	var numbered strings.Builder
	for i, line := range strings.Split(markdown, "\n") {
		fmt.Fprintf(&numbered, "%d| %s\n", i+1, line)
	}
	user = fmt.Sprintf("SOURCE DOCUMENT:\n%s\nCLAIMS:\n%s", numbered.String(), claimsJSON)
	return validatorSystemPrompt, user
}
