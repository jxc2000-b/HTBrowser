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

export type ExtractedClaim = {
  line: number;
  metric: string;
  date: string;
  value: string;
  unit?: string;
  /** 'unverified' covers conversions and repairs as well as fabrications — the user judges. */
  verdict: 'match' | 'unverified';
};

export type UploadReview = {
  original: string;
  extracted: string;
  claims: ExtractedClaim[];
};

/** Sends a raw markdown document for extraction; returns the review payload. */
export async function uploadDocument(filename: string, content: string): Promise<UploadReview> {
  const response = await fetch('/api/upload', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ filename, content }),
  });
  if (!response.ok) {
    throw new Error((await response.text()) || `Upload failed (${response.status}).`);
  }
  return (await response.json()) as UploadReview;
}

/** An incoming reading whose unit contradicts the target file's established unit. */
export type UnitConflict = {
  metric: string;
  /** 1-indexed line in the submitted extracted markdown. */
  line: number;
  text: string;
  unit: string;
  existingUnit: string;
  target: string;
};

/** Thrown by ingestDocument when the merge is rejected over unit mismatches. */
export class IngestConflictError extends Error {
  conflicts: UnitConflict[];

  constructor(conflicts: UnitConflict[]) {
    super('Unit mismatch with existing files.');
    this.conflicts = conflicts;
  }
}

/**
 * Ingests approved (possibly user-edited) canonical markdown into the document bank.
 * assignments maps a heading's metric name to the user's identity resolution:
 * an existing doc ID to merge into, or '' to force a new standalone file.
 * Throws IngestConflictError (nothing written) on unit mismatches.
 */
export async function ingestDocument(
  extracted: string,
  assignments: Record<string, string>,
): Promise<{ files: string[] }> {
  const response = await fetch('/api/ingest', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ extracted, assignments }),
  });
  if (response.status === 409) {
    const payload = (await response.json()) as { conflicts: UnitConflict[] };
    throw new IngestConflictError(payload.conflicts || []);
  }
  if (!response.ok) {
    throw new Error((await response.text()) || `Ingest failed (${response.status}).`);
  }
  return (await response.json()) as { files: string[] };
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
