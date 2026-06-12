import { useEffect, useRef } from 'react';
import type { ValidationResult } from '../api';

type SandboxProps = {
  html: string;
  /** True once the stream has finished — charts render and scripts settle. */
  final: boolean;
  /** Validation results to paint onto claims; null while pending. */
  validation: ValidationResult | null;
  /** Source document name used in citation tooltips. */
  docName: string;
  onNavigate: (href: string) => void;
  title?: string;
};

// Static shell HTML loaded once via srcdoc — runs in an opaque origin (no allow-same-origin).
// Streamed content is pushed in via postMessage instead of resetting srcdoc, so the page
// paints progressively without reloading Tailwind or flashing white between chunks.
// The model never writes <script> tags: charts are declared as data-chart JSON on canvas
// elements and rendered here with Chart.js, so generated pages contain no executable code.
const SHELL_HTML = `<!DOCTYPE html>
<html>
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <meta http-equiv="Content-Security-Policy"
      content="default-src 'none'; script-src 'unsafe-inline' https://cdn.tailwindcss.com https://cdn.jsdelivr.net; style-src 'unsafe-inline' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; img-src data: blob:; connect-src 'none'; frame-src 'none';">
    <script src="https://cdn.tailwindcss.com"></script>
    <script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.7/dist/chart.umd.min.js"></script>
    <script>
      // Hide broken Material Symbols icons.
      // Valid icon ligatures are roughly square (width ≈ height).
      // Broken ones render as text, so width >> height.
      function hideBrokenIcons() {
        document.querySelectorAll(
          '.material-symbols-outlined, .material-symbols-rounded, .material-symbols-sharp, '
          + '.material-icons, .material-icons-outlined, .material-icons-round, .material-icons-sharp, .material-icons-two-tone'
        ).forEach(el => {
          if (el.offsetWidth > el.offsetHeight * 1.5) {
            el.style.display = 'none';
          }
        });
      }

      // Intercept link clicks and delegate navigation to the parent app
      document.addEventListener('click', (e) => {
        const link = e.target.closest('a');
        if (link) {
          e.preventDefault();
          window.parent.postMessage({ type: 'NAVIGATE', href: link.getAttribute('href') || '' }, '*');
        }
      });

      let chartInstances = [];

      function renderCharts() {
        chartInstances.forEach(c => { try { c.destroy(); } catch {} });
        chartInstances = [];
        if (!window.Chart) return;
        document.querySelectorAll('canvas[data-chart]').forEach(canvas => {
          let spec;
          try {
            // Tolerate invalid bare decimals like .9 that some models emit
            const rawJson = (canvas.getAttribute('data-chart') || '')
              .replace(/([\\[,:\\s])\\.(\\d)/g, '$10.$2');
            spec = JSON.parse(rawJson);
          } catch { return; }
          if (!spec || !Array.isArray(spec.values)) return;
          const type = ['line', 'bar', 'doughnut'].includes(spec.type) ? spec.type : 'line';
          const axisColor = getComputedStyle(document.body).color;
          const palette = ['#6366f1', '#22c55e', '#f59e0b', '#ef4444', '#06b6d4', '#a855f7'];
          const baseLabel = (spec.title || '') + (spec.unit ? ' (' + spec.unit + ')' : '');
          // One flat series expected, but tolerate nested per-series arrays
          const seriesList = Array.isArray(spec.values[0]) ? spec.values : [spec.values];
          const datasets = seriesList.map((series, i) => ({
            label: seriesList.length > 1 ? baseLabel + ' #' + (i + 1) : baseLabel,
            data: series,
            borderColor: palette[i % palette.length],
            backgroundColor: type === 'doughnut'
              ? palette
              : 'rgba(99, 102, 241, 0.15)',
            fill: type === 'line' && seriesList.length === 1,
            tension: 0.3,
          }));
          try {
            chartInstances.push(new Chart(canvas, {
              type,
              data: {
                labels: spec.labels || [],
                datasets,
              },
              options: {
                responsive: true,
                maintainAspectRatio: false,
                animation: { duration: 300 },
                plugins: { legend: { labels: { color: axisColor } } },
                scales: type === 'doughnut' ? {} : {
                  x: { ticks: { color: axisColor } },
                  y: { ticks: { color: axisColor } },
                },
              },
            }));
          } catch {}
        });
      }

      // Paint validation results onto claim elements
      function applyValidation(payload) {
        const spans = document.querySelectorAll('span[data-value]');
        const canvases = document.querySelectorAll('canvas[data-chart]');
        const failedCharts = {};

        (payload.claims || []).forEach(claim => {
          if (claim.kind === 'span') {
            const el = spans[claim.spanIndex];
            if (!el) return;
            if (claim.verdict === 'match') {
              el.classList.add('hd-verified');
              el.title = 'Verified · ' + payload.docName + '.md, line ' + claim.line;
            } else {
              el.classList.add('hd-failed');
              el.title = 'UNVERIFIED · ' + (claim.note || 'not found in ' + payload.docName + '.md');
            }
          } else if (claim.kind === 'chart' && claim.verdict !== 'match') {
            failedCharts[claim.chartIndex] = claim.note || 'chart value not found in source';
          }
        });

        canvases.forEach((canvas, i) => {
          const wrapper = canvas.parentElement || canvas;
          if (failedCharts[i] !== undefined) {
            wrapper.classList.add('hd-chart-failed');
            wrapper.title = 'UNVERIFIED chart data · ' + failedCharts[i];
          } else {
            wrapper.classList.add('hd-chart-verified');
          }
        });

        document.body.classList.add('hd-validated');
      }

      // Receive updates from the parent
      window.addEventListener('message', (e) => {
        if (e.data?.type === 'CONTENT_UPDATE') {
          document.body.innerHTML = e.data.html;
          document.body.className = 'min-h-screen ' + (e.data.bodyClasses || '');
          document.body.setAttribute('style', e.data.bodyStyle || '');
          document.documentElement.style.colorScheme = e.data.colorScheme || 'light';

          // Inject font links (only pre-validated Google Fonts hrefs)
          document.head.querySelectorAll('link[data-health-dash-font]').forEach(el => el.remove());
          (e.data.linkTags || []).forEach(href => {
            const link = document.createElement('link');
            link.rel = 'stylesheet';
            link.href = href;
            link.setAttribute('data-health-dash-font', 'true');
            document.head.appendChild(link);
          });

          if (e.data.final) {
            renderCharts();
          }

          // After content + fonts are ready, hide any broken icon ligatures
          document.fonts.ready.then(() => hideBrokenIcons());
        }
        if (e.data?.type === 'VALIDATION_UPDATE') {
          applyValidation(e.data);
        }
      });

      // Signal ready to parent
      window.parent.postMessage({ type: 'SANDBOX_READY' }, '*');
    </script>
    <link href="https://fonts.googleapis.com/css2?family=Material+Symbols+Outlined:opsz,wght,FILL,GRAD@20..48,100..700,0..1,-50..200" rel="stylesheet" />
    <style>
      html { font-family: Helvetica, Arial, sans-serif; }
      body { -webkit-font-smoothing: antialiased; }
      input, textarea, select, button { color: inherit; }
      ::placeholder { opacity: 0.5; }

      /* Claim states: dashed = awaiting validation, solid = verified, red = failed */
      span[data-value] {
        border-bottom: 1px dashed rgba(125, 125, 125, 0.55);
        cursor: help;
      }
      span[data-value].hd-verified {
        border-bottom: 1px solid rgba(34, 197, 94, 0.45);
      }
      span[data-value].hd-failed {
        border-bottom: 1px solid rgba(239, 68, 68, 0.9);
        background: rgba(239, 68, 68, 0.12);
        text-decoration: line-through;
      }
      .hd-chart-failed {
        outline: 2px solid rgba(239, 68, 68, 0.7);
        outline-offset: 2px;
        border-radius: 4px;
      }

      /* Handle all Material Icon class variants the model might use */
      .material-symbols-outlined,
      .material-symbols-rounded,
      .material-symbols-sharp,
      .material-icons,
      .material-icons-outlined,
      .material-icons-round,
      .material-icons-sharp,
      .material-icons-two-tone {
        font-family: 'Material Symbols Outlined';
        font-weight: normal;
        font-style: normal;
        font-size: 24px;
        line-height: 1;
        letter-spacing: normal;
        text-transform: none;
        display: inline-block;
        white-space: nowrap;
        word-wrap: normal;
        direction: ltr;
        -webkit-font-smoothing: antialiased;
        -moz-osx-font-smoothing: grayscale;
        font-feature-settings: 'liga';
      }
    </style>
  </head>
  <body class="min-h-screen" style="background-color: #f6f7f9;">
    <main style="min-height: 100vh; display: grid; place-items: center; color: #2f3747;">
      <div style="max-width: 560px; padding: 32px; text-align: center;">
        <h1 style="margin: 0 0 8px; font-size: 24px;">Health dashboard</h1>
        <p style="margin: 0; color: #687386; line-height: 1.5;">Generating your dashboard…</p>
      </div>
    </main>
  </body>
</html>`;

type ShellMessage =
  | {
      type: 'CONTENT_UPDATE';
      html: string;
      bodyClasses: string;
      bodyStyle: string;
      colorScheme: string;
      linkTags: string[];
      final: boolean;
    }
  | {
      type: 'VALIDATION_UPDATE';
      claims: ValidationResult['claims'];
      docName: string;
    };

export function Sandbox({ html, final, validation, docName, onNavigate, title = 'Generated dashboard' }: SandboxProps) {
  const iframeRef = useRef<HTMLIFrameElement | null>(null);
  const iframeReadyRef = useRef(false);
  const pendingRef = useRef<ShellMessage[]>([]);
  const onNavigateRef = useRef(onNavigate);
  onNavigateRef.current = onNavigate;

  const send = (message: ShellMessage) => {
    if (iframeReadyRef.current && iframeRef.current?.contentWindow) {
      iframeRef.current.contentWindow.postMessage(message, '*');
    } else {
      pendingRef.current.push(message);
    }
  };

  // Push streamed content into the shell as it arrives
  useEffect(() => {
    if (!html) return;

    // Some models wrap output in markdown fences despite instructions — strip them
    const raw = html.replace(/^\s*```[a-z]*\s*/i, '').replace(/```\s*$/, '');

    // While the document head is still streaming in, there is nothing renderable
    // yet — wait for the <body> tag instead of flashing raw head markup as text
    if (/<head[\s>]/i.test(raw) && !/<body[\s>]/i.test(raw)) return;

    // Detect color scheme from meta tag before stripping head
    const isDark = /<meta\s+name=["']color-scheme["']\s+content=["']dark["']/i.test(raw);

    // Extract <link> tags from <head> — only allow Google Fonts URLs
    const headMatch = raw.match(/<head[^>]*>([\s\S]*?)<\/head>/i);
    const fontHrefs: string[] = [];
    if (headMatch) {
      const linkMatches = headMatch[1].match(/<link[^>]*>/gi);
      if (linkMatches) {
        linkMatches.forEach((tag) => {
          const hrefMatch = tag.match(/href="([^"]+)"/i) || tag.match(/href='([^']+)'/i);
          if (hrefMatch && hrefMatch[1].startsWith('https://fonts.googleapis.com/')) {
            fontHrefs.push(hrefMatch[1]);
          }
        });
      }
    }

    // Extract body class attribute (for Tailwind classes)
    const bodyClassMatch = raw.match(/<body[^>]*class="([^"]*)"/i) || raw.match(/<body[^>]*class='([^']*)'/i);
    const bodyClasses = bodyClassMatch ? bodyClassMatch[1] : '';

    // Extract body inline style (for font-family etc.)
    const bodyStyleMatch = raw.match(/<body[^>]*style="([^"]*)"/i) || raw.match(/<body[^>]*style='([^']*)'/i);
    const bodyInlineStyle = bodyStyleMatch ? bodyStyleMatch[1] : '';

    // Extract just the body content from a full HTML document
    let cleanContent = raw;
    const bodyMatch = cleanContent.match(/<body[^>]*>([\s\S]*?)(<\/body>|$)/i);
    if (bodyMatch) {
      cleanContent = bodyMatch[1];
    } else {
      // Fallback: strip any stray tags from fragment-style output
      cleanContent = cleanContent
        .replace(/<\/?html[^>]*>/gi, '')
        .replace(/<head>[\s\S]*?<\/head>/gi, '')
        .replace(/<title>[^<]*<\/title>/gi, '')
        .replace(/<meta[^>]*>/gi, '')
        .replace(/<\/?body[^>]*>/gi, '');
    }

    send({
      type: 'CONTENT_UPDATE',
      html: cleanContent,
      bodyClasses,
      bodyStyle: `background-color: ${isDark ? '#111' : '#fff'}; color: ${isDark ? '#e8eaed' : '#1a1a1a'}; ${bodyInlineStyle}`,
      colorScheme: isDark ? 'dark' : 'light',
      linkTags: fontHrefs,
      final,
    });

    // Update the iframe element's own background to match (prevents white flash)
    if (iframeRef.current) {
      iframeRef.current.style.background = isDark ? '#111' : '#fff';
    }
  }, [html, final]);

  // Forward validation results once available
  useEffect(() => {
    if (!validation) return;
    send({ type: 'VALIDATION_UPDATE', claims: validation.claims, docName });
  }, [validation, docName]);

  // Handle messages from the shell (ready handshake + navigation)
  useEffect(() => {
    const handler = (event: MessageEvent) => {
      if (event.source !== iframeRef.current?.contentWindow) return;
      if (event.data?.type === 'SANDBOX_READY') {
        iframeReadyRef.current = true;
        const pending = pendingRef.current;
        pendingRef.current = [];
        pending.forEach((message) => iframeRef.current?.contentWindow?.postMessage(message, '*'));
      }
      if (event.data?.type === 'NAVIGATE') {
        onNavigateRef.current(String(event.data.href || ''));
      }
    };
    window.addEventListener('message', handler);
    return () => window.removeEventListener('message', handler);
  }, []);

  return (
    <iframe
      ref={iframeRef}
      className="sandbox-frame"
      title={title}
      sandbox="allow-scripts allow-forms"
      srcDoc={SHELL_HTML}
    />
  );
}
