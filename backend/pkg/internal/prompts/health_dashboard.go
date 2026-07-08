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
  <title>HTBROWSER - Page Name (no subtitle yet)</title>
  <meta name="color-scheme" content="light">
  <link href="https://fonts.googleapis.com/css2?family=ChosenFont:wght@400;500;600;700&display=swap" rel="stylesheet">
</head>
<body style="font-family: 'Chosen Font', sans-serif">
  ...page content...
</body>
</html>

Keep the <head> minimal — just the <title>, <meta name="color-scheme">, and a Google Fonts <link>. Tailwind CSS and scripts are injected automatically.
ALWAYS use a light color scheme: set <meta name="color-scheme" content="light"> and design the page for a light background. Never use a dark theme.

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
- ALWAYS display numbers as digits, never as words. Write "180", never "one hundred and eighty"; write "84", never "eighty-four". If the document itself spells a number out in words, convert it to digits for display (e.g. the document says "twenty-eight ng/ml" → display 28).
- Health values appear ONLY inside chart data-chart JSON payloads (see CHARTS below) — never as text on the page.
- If a value you want to plot is not in the document, leave it out; never fill gaps with estimates.

CHARTS (critical):
Where the document contains a time series or comparable values, include charts. Do NOT write any JavaScript. Instead emit a canvas element whose data-chart attribute is single-quoted JSON, and the host page renders it with Chart.js:

  <div class="h-64"><canvas data-chart='{"type":"line","title":"TSH over time","unit":"mIU/L","labels":["2025-01-15","2025-06-02"],"values":[4.2,3.8]}'></canvas></div>

- "type" is one of: line, bar, doughnut.
- "labels" and "values" must come verbatim from the document and stay in document order.
- Each chart plots exactly ONE metric series: "values" is a flat array of numbers. Use a separate chart per metric; never nest arrays.
- ONLY plot values that belong to the same metric measured in the same unit over time (e.g. TSH across several dates). NEVER combine different, unrelated metrics into one chart — do not put HbA1c, ferritin, and vitamin D (or cholesterol and glucose, etc.) together on a single chart, even as a "summary". Different metrics with different units are not comparable and such a chart is meaningless. If a metric has only one reading, show it as a stat, not a chart.
- A bar chart comparing related components measured in the SAME unit (e.g. total cholesterol vs LDL vs HDL, all mg/dL) is acceptable; mixing units is not.
- Write valid JSON numbers with leading zeros (0.9, never .9). Keep the document's formatting: if the document says 5.10, write 5.10, not 5.1.
- Always wrap the canvas in a div with a Tailwind height class (e.g. h-64).
- Include a chart for EVERY metric the document supports: any metric with two or more comparable readings gets its own chart.
- A metric with only a single reading is OMITTED entirely — no chart, no stat, no text mention.

CONTENT (simple view — charts only):
The page contains ONLY charts. Render a minimal header — the page title, the source document name, and a link back to home — followed by a responsive grid of chart cards, and nothing else.
STRICTLY FORBIDDEN on the page:
- Headline stats, stat tiles, or standalone displayed values of any kind.
- Tables of readings or any other tabular data.
- "Visualization notes", definitions, summaries, interpretations, or any prose sections.
- Any health data value rendered as text outside a chart. Because of this, the page should contain NO claim spans — every value on the page lives inside a data-chart JSON payload.
The only text on the page is the header and each chart's title (with its unit).`

const validatorSystemPrompt = `You are a strict data validator for a health dashboard. You verify that values displayed on a generated page exist in a source markdown document.

You are given:
1. The source document with line numbers, formatted as "N| content".
2. A JSON array of claims extracted from the rendered page: {"id", "value", "label", "unit", "date"}.

For each claim, find the line in the document that contains that exact value used in that context (matching label/date where given), and confirm the unit is correct.

Respond with ONLY a JSON array, no prose, no markdown fences:
[{"id": 0, "line": 12, "verdict": "match"}, {"id": 1, "line": 0, "verdict": "no_match", "note": "value not in document"}]

Rules:
- "match" requires the value to appear on the cited line. Cite the single most relevant line number.
- If the value appears nowhere, or only in a different context (wrong metric, wrong date), the verdict is "no_match" with line 0 and a brief note.
- Be strict about VALUES: a rounded, converted, or computed value that does not appear in the document is "no_match".
- UNITS: if the claim's unit contradicts the unit the document gives for that value (e.g. claim says mg/dL but the document says mmol/L, or the document gives no unit and the claim invents one), the verdict is "no_match" with a note naming the unit problem.
- Pure formatting differences are NOT mismatches: 5.10 matches a claim of 5.1, and 0.9 matches .9 — the number is identical. Only genuine rounding or changed digits (3.93 vs 3.9) fail.
- Numbers written as WORDS in the document equal the same number as digits: "twenty-eight" (or "twentyeight") matches a claim of 28, "eighty-four" matches 84, "one hundred and eighty" matches 180. Treat them as identical.
- A range written as "9-23", "(9 - 23)", or "0.35 - 4.94" CONTAINS both endpoints: a claim of 23 matches the line "BUN/CREAT RATIO 15 (9-23)", and a claim of 9 matches it too.
- Never invent line numbers.`

const extractorSystemPrompt = `You convert a raw personal health document into a canonical measurement markdown file.

OUTPUT FORMAT (strict). For each metric found in the document:

# METRIC NAME
range: 0 - 199 mg/dL
2025-02-10 153 mg/dL
2026-06-09 175 mg/dL

- One metric per "# " heading, named as the document names it.
- The "range:" line is included ONLY if the document provides a reference range for that metric; keep its unit.
- One reading per line: ISO date (YYYY-MM-DD), then the value, then the unit if the document has one. Nothing else on the line.
- Readings stay in chronological order.

RULES:
- Normalize dates to YYYY-MM-DD.
- Write numbers as digits with dot decimals (81,4 becomes 81.4; "twenty-eight" becomes 28).
- You MAY convert units, but use ONE unit consistently within a metric — prefer the unit the document uses most for that metric. Keep the source's precision; do not add invented digits.
- Skip missing readings (cells like "-", "_", or blank). Never estimate or fill gaps.
- Small repairs are allowed (obvious typos, misaligned table cells), inventing data is not.
- Output ONLY the canonical markdown: no prose, no tables, no code fences, no commentary.`

// ExtractorMessages builds the prompt pair for converting an uploaded raw
// document into the canonical measurement format.
func ExtractorMessages(filename, content string) (system string, user string) {
	user = fmt.Sprintf("Convert this document (%s) into canonical measurement markdown.\n\n<<<DOCUMENT\n%s\nDOCUMENT", filename, content)
	return extractorSystemPrompt, user
}

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
	sb.WriteString("\n\nRemember: the page is charts only — no stats, tables, or notes. Charts use data-chart JSON, and nothing may be plotted that is not verbatim in the document.")

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
