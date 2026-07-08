export type GenerateOptions = {
  /** Document ID to render; omit for the homepage. */
  doc?: string;
  /** Values that failed validation on a previous attempt (repair pass). */
  repairNotes?: string[];
  /** Bypass the server's homepage cache (explicit regeneration). */
  force?: boolean;
};

type GenerateHandlers = {
  onStart?: (status: string) => void;
  onDelta: (text: string) => void;
  onError: (message: string) => void;
  onDone: () => void;
};

export type StreamHandle = {
  abort: () => void;
};

export type ClaimResult = {
  id: number;
  kind: 'span' | 'chart';
  spanIndex?: number;
  chartIndex?: number;
  value: string;
  label?: string;
  unit?: string;
  date?: string;
  line: number;
  /** 'unverified' = validator LLM was unavailable; value passed the presence gate but was not context-checked. */
  verdict: 'match' | 'no_match' | 'citation_mismatch' | 'unverified';
  note?: string;
};

export type DocInfo = {
  id: string;
  title: string;
  summary: string;
};

/** Fetches the document registry directly — no LLM round trip — for instant navigation. */
export async function fetchDocs(): Promise<DocInfo[]> {
  const response = await fetch('/api/docs');
  if (!response.ok) {
    throw new Error(`Document list request failed (${response.status}).`);
  }
  return (await response.json()) as DocInfo[];
}

export type ValidationResult = {
  status: 'verified' | 'partial' | 'no_claims';
  claims: ClaimResult[];
};

// Streams the generated page over SSE. Uses fetch + ReadableStream instead of
// EventSource so the request can be a POST carrying doc/repair parameters.
export function streamGeneratedPage(options: GenerateOptions, handlers: GenerateHandlers): StreamHandle {
  const controller = new AbortController();

  (async () => {
    try {
      const response = await fetch('/api/generate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          doc: options.doc || '',
          repairNotes: options.repairNotes || [],
          force: options.force || false,
        }),
        signal: controller.signal,
      });

      if (!response.ok || !response.body) {
        handlers.onError(`Generation request failed (${response.status}).`);
        return;
      }

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';
      let sawDone = false;

      const handleEvent = (block: string) => {
        let event = 'message';
        let data = '';
        for (const line of block.split('\n')) {
          if (line.startsWith('event:')) event = line.slice(6).trim();
          if (line.startsWith('data:')) data += line.slice(5).trim();
        }
        let payload: { status?: string; text?: string; message?: string } = {};
        try {
          payload = data ? JSON.parse(data) : {};
        } catch {
          return;
        }
        if (event === 'start') handlers.onStart?.(payload.status || 'generating');
        if (event === 'delta' && payload.text) handlers.onDelta(payload.text);
        if (event === 'error') {
          sawDone = true;
          handlers.onError(payload.message || 'Generation failed.');
        }
        if (event === 'done') {
          sawDone = true;
          handlers.onDone();
        }
      };

      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        let boundary = buffer.indexOf('\n\n');
        while (boundary !== -1) {
          handleEvent(buffer.slice(0, boundary));
          buffer = buffer.slice(boundary + 2);
          boundary = buffer.indexOf('\n\n');
        }
        if (sawDone) return;
      }

      if (!sawDone) {
        handlers.onError('Generation stream disconnected.');
      }
    } catch (error) {
      if (controller.signal.aborted) return;
      handlers.onError(error instanceof Error ? error.message : 'Generation stream failed.');
    }
  })();

  return { abort: () => controller.abort() };
}

export async function validatePage(doc: string, html: string): Promise<ValidationResult> {
  const response = await fetch('/api/validate', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ doc, html }),
  });
  if (!response.ok) {
    throw new Error(`Validation request failed (${response.status}).`);
  }
  return (await response.json()) as ValidationResult;
}
