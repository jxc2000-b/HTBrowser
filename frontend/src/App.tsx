import { useEffect, useRef, useState } from 'react';
import { streamGeneratedPage, validatePage, type StreamHandle, type ValidationResult } from './api';
import { Sandbox } from './components/Sandbox';
import './styles/styles.css';

// Models stream many tiny deltas per second; re-rendering the sandbox for each
// one saturates the iframe's main thread and it never gets a chance to paint.
// Coalesce deltas and flush to React state at most every FLUSH_INTERVAL_MS.
const FLUSH_INTERVAL_MS = 100;

type Status = 'idle' | 'generating' | 'validating' | 'verified' | 'partial' | 'done' | 'error';

function App() {
  const [html, setHtml] = useState('');
  const [status, setStatus] = useState<Status>('idle');
  const [error, setError] = useState('');
  const [chunkCount, setChunkCount] = useState(0);
  const [doc, setDoc] = useState(''); // '' = homepage
  const [validation, setValidation] = useState<ValidationResult | null>(null);
  const [repairUsed, setRepairUsed] = useState(false);

  const streamRef = useRef<StreamHandle | null>(null);
  const bufferRef = useRef({ html: '', chunks: 0 });
  const flushTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const flushBuffer = () => {
    if (flushTimerRef.current !== null) {
      clearTimeout(flushTimerRef.current);
      flushTimerRef.current = null;
    }
    setHtml(bufferRef.current.html);
    setChunkCount(bufferRef.current.chunks);
  };

  const generate = (targetDoc: string, repairNotes?: string[], force?: boolean) => {
    streamRef.current?.abort();
    bufferRef.current = { html: '', chunks: 0 };
    flushBuffer();
    setError('');
    setValidation(null);
    setDoc(targetDoc);
    setRepairUsed(Boolean(repairNotes?.length));
    setStatus('generating');

    const isRepair = Boolean(repairNotes?.length);

    streamRef.current = streamGeneratedPage(
      { doc: targetDoc || undefined, repairNotes, force },
      {
        onDelta: (text) => {
          bufferRef.current.html += text;
          bufferRef.current.chunks += 1;
          if (flushTimerRef.current === null) {
            flushTimerRef.current = setTimeout(flushBuffer, FLUSH_INTERVAL_MS);
          }
        },
        onError: (message) => {
          flushBuffer();
          setError(message);
          setStatus('error');
          streamRef.current = null;
        },
        onDone: () => {
          flushBuffer();
          streamRef.current = null;
          if (targetDoc) {
            // A repair pass gets validated too, but cannot trigger another repair.
            void runValidation(targetDoc, !isRepair);
          } else {
            setStatus('done'); // homepage shows no data values, nothing to validate
          }
        },
      },
    );
  };

  const runValidation = async (targetDoc: string, repairAllowed: boolean) => {
    setStatus('validating');
    try {
      const result = await validatePage(targetDoc, bufferRef.current.html);
      const failed = result.claims.filter((claim) => claim.verdict !== 'match');

      if (failed.length > 0 && repairAllowed) {
        const notes = failed.map((claim) => {
          const parts = [claim.value];
          if (claim.unit) parts.push(claim.unit);
          if (claim.label) parts.push(`(${claim.label}${claim.date ? `, ${claim.date}` : ''})`);
          return `${parts.join(' ')}${claim.note ? ` — ${claim.note}` : ''}`;
        });
        generate(targetDoc, notes);
        return;
      }

      setValidation(result);
      setStatus(failed.length > 0 ? 'partial' : 'verified');
    } catch (validationError) {
      setError(validationError instanceof Error ? validationError.message : 'Validation failed.');
      setStatus('error');
    }
  };

  const stop = () => {
    streamRef.current?.abort();
    streamRef.current = null;
    flushBuffer();
    if (status === 'generating') {
      setStatus(bufferRef.current.html ? 'done' : 'idle');
    }
  };

  const navigate = (href: string) => {
    if (status === 'generating' || status === 'validating') return;
    if (href === '/' || href === '' || href === '/home') {
      generate('');
      return;
    }
    const docMatch = href.match(/^\/?doc\/([a-zA-Z0-9][a-zA-Z0-9_-]*)/) || href.match(/^\/([a-zA-Z0-9][a-zA-Z0-9_-]*)$/);
    if (docMatch) {
      generate(docMatch[1]);
    }
  };

  // Load the homepage on first visit
  useEffect(() => {
    generate('');
  }, []);

  const isGenerating = status === 'generating';
  const failedClaims = validation?.claims.filter((claim) => claim.verdict !== 'match') ?? [];

  const statusLabel: Record<Status, string> = {
    idle: 'Ready',
    generating: 'Streaming',
    validating: 'Validating data…',
    verified: 'Data verified',
    partial: 'Unverified data',
    done: 'Rendered',
    error: 'Error',
  };

  return (
    <main className="app-shell">
      <section className="toolbar">
        <div>
          <p className="eyebrow">Health Dash</p>
          <h1>{doc ? `${doc}.md` : 'Home'}</h1>
        </div>

        <div className="actions">
          <span className={`status-pill status-${status}`}>{statusLabel[status]}</span>
          {doc && !isGenerating && (
            <button type="button" className="secondary-button" onClick={() => generate('')}>
              Home
            </button>
          )}
          {isGenerating ? (
            <button type="button" className="secondary-button" onClick={stop}>
              Stop
            </button>
          ) : (
            <button type="button" className="primary-button" onClick={() => generate(doc, undefined, true)}>
              Regenerate
            </button>
          )}
        </div>
      </section>

      {error && <div className="error-banner">{error}</div>}

      {(status === 'generating' || status === 'validating') && doc && (
        <div className="notice-banner">
          {status === 'validating'
            ? `Checking every displayed value against ${doc}.md — dashed values are not yet verified.`
            : `Streaming${repairUsed ? ' (repair pass)' : ''}… values shown are unverified until validation completes.`}
        </div>
      )}

      {status === 'partial' && (
        <div className="error-banner">
          {failedClaims.length} value{failedClaims.length === 1 ? '' : 's'} could not be verified against {doc}.md
          {repairUsed ? ' (after a repair pass)' : ''} and {failedClaims.length === 1 ? 'is' : 'are'} flagged in red:
          <ul>
            {failedClaims.map((claim) => (
              <li key={claim.id}>
                {claim.value}
                {claim.unit ? ` ${claim.unit}` : ''}
                {claim.label ? ` — ${claim.label}` : ''}
                {claim.note ? ` (${claim.note})` : ''}
              </li>
            ))}
          </ul>
        </div>
      )}

      <section className="meta-row" aria-live="polite">
        <span>{html.length.toLocaleString()} characters streamed</span>
        <span>{chunkCount.toLocaleString()} chunks</span>
        {validation && status === 'verified' && (
          <span>{validation.claims.length} values verified against {doc}.md</span>
        )}
      </section>

      <section className="preview-shell">
        <Sandbox
          html={html}
          final={!isGenerating && status !== 'idle'}
          validation={validation}
          docName={doc}
          onNavigate={navigate}
        />
      </section>
    </main>
  );
}

export default App;
