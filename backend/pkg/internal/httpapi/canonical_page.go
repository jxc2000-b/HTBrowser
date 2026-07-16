package httpapi

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"health-dash/pkg/internal/docs"
)

// isCanonical reports whether a document is in the canonical measurement
// format: at least one "# METRIC" section, every section carrying at least
// one parseable reading. Raw dumps and prose fail this and take the LLM path.
func isCanonical(markdown string) bool {
	chunks := parseCanonical(markdown)
	if len(chunks) == 0 {
		return false
	}
	for _, chunk := range chunks {
		if len(chunk.Readings) == 0 {
			return false
		}
	}
	return true
}

// canonicalPage renders the charts-only dashboard page for a canonical
// document purely programmatically — no LLM, no hallucination surface, no
// tokens. The output uses the exact same contract as generated pages
// (Tailwind classes, data-chart canvases, /-relative links), so the sandbox
// shell and the validator treat it identically. Returns ok=false when the
// document is not canonical.
func canonicalPage(info docs.DocInfo, markdown string) (string, bool) {
	if !isCanonical(markdown) {
		return "", false
	}

	var charts strings.Builder
	for _, chunk := range parseCanonical(markdown) {
		charts.WriteString(chartSection(chunk, ""))
	}

	body := charts.String()
	if body == "" {
		body = `<p class="text-slate-500">No metric in this document has enough readings to chart yet.</p>`
	}

	subtitle := fmt.Sprintf("source: %s.md · rendered without AI, values are read directly from the file", html.EscapeString(info.ID))
	return pageShell(info.Title, subtitle, body), true
}

// overviewPage renders every chartable metric across all canonical documents
// in one page. Each chart title links to the metric's own page. Non-canonical
// (legacy) documents are skipped — their values are not machine-readable.
func overviewPage(registry docs.Registry, infos []docs.DocInfo) string {
	var charts strings.Builder
	skipped := 0
	for _, info := range infos {
		markdown, err := registry.Read(info.ID)
		if err != nil || !isCanonical(markdown) {
			skipped++
			continue
		}
		for _, chunk := range parseCanonical(markdown) {
			charts.WriteString(chartSection(chunk, "/doc/"+info.ID))
		}
	}

	body := charts.String()
	if body == "" {
		body = `<p class="text-slate-500">No metric has enough readings to chart yet — upload some data first.</p>`
	}
	if skipped > 0 {
		body += fmt.Sprintf(`
    <p class="text-xs text-slate-400">%d document%s not in the canonical measurement format %s omitted.</p>`,
			skipped, plural(skipped), pluralWas(skipped))
	}

	subtitle := "every metric across all documents · rendered without AI, values are read directly from the files"
	return pageShell("Overview", subtitle, body)
}

// chartSection renders one metric's chart card. Metrics with fewer than two
// readings render nothing — same rule as the generation prompt. titleHref,
// when non-empty, wraps the card title in a link.
func chartSection(chunk metricChunk, titleHref string) string {
	if len(chunk.Readings) < 2 {
		return ""
	}
	unit := ""
	for _, r := range chunk.Readings {
		if r.Unit != "" {
			unit = r.Unit
			break
		}
	}
	labels := make([]string, len(chunk.Readings))
	values := make([]string, len(chunk.Readings))
	for i, r := range chunk.Readings {
		labels[i] = r.Date
		// Raw value tokens go into the JSON verbatim (5.10 stays 5.10) so
		// the validator sees the document's own formatting.
		values[i] = strings.ReplaceAll(r.Value, ",", "")
	}
	labelsJSON, _ := json.Marshal(labels)
	spec := fmt.Sprintf(`{"type":"line","title":%s,"unit":%s,"labels":%s,"values":[%s]}`,
		mustJSON(chunk.Metric), mustJSON(unit), labelsJSON, strings.Join(values, ","))

	title := html.EscapeString(chunk.Metric) + unitSuffix(unit)
	if titleHref != "" {
		title = fmt.Sprintf(`<a href="%s" class="hover:underline">%s</a>`, html.EscapeString(titleHref), title)
	}

	return fmt.Sprintf(`
    <section class="w-full bg-white border border-slate-200 rounded-lg p-4 mb-6">
      <h2 class="text-sm font-semibold text-slate-700 mb-2">%s</h2>
      <div class="h-64 w-full"><canvas data-chart='%s'></canvas></div>
    </section>`,
		title,
		escapeSingleQuotedAttr(spec))
}

// pageShell wraps page content in the shared document skeleton: title,
// subtitle, home link, light color scheme, Tailwind-styled body.
func pageShell(title, subtitle, body string) string {
	return fmt.Sprintf(`<html>
<head>
  <title>%s</title>
  <meta name="color-scheme" content="light">
</head>
<body class="bg-slate-50 text-slate-900">
  <main class="max-w-5xl mx-auto px-6 py-8">
    <header class="mb-6 flex items-baseline justify-between gap-4">
      <div>
        <h1 class="text-xl font-bold">%s</h1>
        <p class="text-xs text-slate-500">%s</p>
      </div>
      <a href="/" class="text-sm font-semibold text-blue-700 hover:underline">Home</a>
    </header>
    %s
  </main>
</body>
</html>`,
		html.EscapeString(title),
		html.EscapeString(title),
		subtitle,
		body)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func pluralWas(n int) string {
	if n == 1 {
		return "was"
	}
	return "were"
}

// homePage renders the homepage programmatically from the registry: a
// compact table-of-contents list ("Title: reference range") and a
// disclaimer. Like canonical doc pages, there is nothing here an LLM could
// add — every fact comes straight from the registry.
func homePage(registry []docs.DocInfo) string {
	var rows strings.Builder
	for _, info := range registry {
		label := html.EscapeString(info.Title)
		// The summary is the file's first content line; for canonical metric
		// files that is the "range:" line, which is worth showing. Anything
		// else stays off the page.
		detail := ""
		if strings.HasPrefix(strings.ToLower(info.Summary), "range:") {
			detail = ": " + html.EscapeString(strings.TrimSpace(info.Summary[len("range:"):]))
		}
		rows.WriteString(fmt.Sprintf(
			"\n      <li><a href=\"/doc/%s\" class=\"font-semibold text-blue-700 hover:underline\">%s</a><span class=\"text-slate-500\">%s</span></li>",
			html.EscapeString(info.ID), label, detail))
	}
	if rows.Len() == 0 {
		rows.WriteString(`
      <li class="text-slate-500">No documents yet — upload one to get started.</li>`)
	}

	overviewLink := ""
	if len(registry) > 0 {
		overviewLink = `
    <p class="mb-4 text-sm"><a href="/doc/overview" class="font-semibold text-blue-700 hover:underline">Overview</a><span class="text-slate-500"> — all metrics on one page</span></p>`
	}

	return fmt.Sprintf(`<html>
<head>
  <title>Home</title>
  <meta name="color-scheme" content="light">
</head>
<body class="bg-slate-50 text-slate-900">
  <main class="max-w-2xl mx-auto px-6 py-10">
    <h1 class="text-xl font-bold mb-4">Documents</h1>%s
    <ul class="space-y-1 text-sm leading-6">%s
    </ul>
    <p class="mt-8 text-xs text-slate-400">This tool only visualizes data you provided. It is not medical advice; check values against your original documents.</p>
  </main>
</body>
</html>`, overviewLink, rows.String())
}

func unitSuffix(unit string) string {
	if unit == "" {
		return ""
	}
	return html.EscapeString(" (" + unit + ")")
}

func mustJSON(s string) string {
	payload, _ := json.Marshal(s)
	return string(payload)
}

// escapeSingleQuotedAttr makes a string safe inside a single-quoted HTML
// attribute, preserving the double quotes JSON needs.
func escapeSingleQuotedAttr(s string) string {
	return strings.NewReplacer("&", "&amp;", "'", "&#39;").Replace(s)
}
