import { useRef, useState, type ChangeEvent } from 'react';
import type { UnitConflict, UploadReview as UploadReviewData } from '../api';

type UploadReviewProps = {
  review: UploadReviewData;
  /** Existing document IDs — the metric identity registry headings are matched against. */
  registry: string[];
  /** Unit mismatches from a rejected ingest; the offending unit gets a red underline. */
  unitConflicts: UnitConflict[];
  busy: boolean;
  onApprove: (extracted: string, assignments: Record<string, string>) => void;
  onCancel: () => void;
};

/** The user's identity decision for one heading. */
type Resolution = {
  chosen?: string;
  /** True when the user rejected all matches: the heading becomes a new standalone file. */
  rejected?: boolean;
};

const words = (text: string): string[] =>
  text
    .toLowerCase()
    .split(/[^a-z0-9]+/)
    .filter(Boolean);

// A registry entry is a candidate for a heading when they share at least one
// word ("HDL CHOLESTEROL" matches both "hdl" and "cholesterol"). Matching is
// case- and whitespace-insensitive by construction.
const candidatesFor = (heading: string, registry: string[]): string[] => {
  const headingWords = new Set(words(heading));
  return registry.filter((id) => words(id).some((word) => headingWords.has(word)));
};

// Side-by-side review of an upload: the untouched original on the left, the
// extracted canonical markdown on the right in an editable pane with
// code-editor line numbers. Reading lines whose values were not found in the
// original get a red underline. Heading lines are identity-matched against
// the registry of existing metric files: a single match colors the line
// number green, multiple matches glow yellow and must be resolved (pick one
// or reject) before approving. User edits are terminal.
export function UploadReview({ review, registry, unitConflicts, busy, onApprove, onCancel }: UploadReviewProps) {
  const [extracted, setExtracted] = useState(review.extracted);
  const [resolutions, setResolutions] = useState<Record<string, Resolution>>({});
  const [popup, setPopup] = useState<{ heading: string; x: number; y: number } | null>(null);

  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const backdropRef = useRef<HTMLPreElement | null>(null);
  const gutterRef = useRef<HTMLDivElement | null>(null);
  const closeTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const unverifiedClaims = review.claims.filter((claim) => claim.verdict === 'unverified');

  // Flag reading lines by their content as originally extracted, not by line
  // index: indexes shift as soon as the user edits, and a corrected line
  // should stop being flagged anyway.
  const originalLines = review.extracted.split('\n');
  const unverifiedTexts = new Set(
    unverifiedClaims
      .map((claim) => originalLines[claim.line - 1]?.trim())
      .filter((text): text is string => Boolean(text)),
  );

  const extractedLines = extracted.split('\n');
  const originalDocLines = review.original.split('\n');

  // Unit conflicts are keyed by line content, like value flags: editing the
  // line (converting the value, fixing the unit) clears its marking.
  const conflictByText = new Map(unitConflicts.map((conflict) => [conflict.text.trim(), conflict]));

  // Renders a conflicted line with only the offending unit underlined.
  const renderLine = (line: string) => {
    const conflict = conflictByText.get(line.trim());
    if (!conflict || !conflict.unit) return line || ' ';
    const at = line.lastIndexOf(conflict.unit);
    if (at === -1) return line;
    return (
      <>
        {line.slice(0, at)}
        <span
          className="review-unit-flagged"
          title={`Unit mismatch: ${conflict.target}.md uses ${conflict.existingUnit}`}
        >
          {conflict.unit}
        </span>
        {line.slice(at + conflict.unit.length)}
      </>
    );
  };

  const headingOf = (line: string): string | null =>
    line.trim().startsWith('# ') ? line.trim().slice(2).trim() : null;

  type HeadingState = 'matched' | 'ambiguous' | 'rejected' | 'none';
  const headingState = (heading: string): { state: HeadingState; target?: string } => {
    const candidates = candidatesFor(heading, registry);
    const resolution = resolutions[heading];
    if (candidates.length === 0) return { state: 'none' };
    if (resolution?.rejected) return { state: 'rejected' };
    if (resolution?.chosen) return { state: 'matched', target: resolution.chosen };
    if (candidates.length === 1) return { state: 'matched', target: candidates[0] };
    return { state: 'ambiguous' };
  };

  const unresolved = extractedLines
    .map(headingOf)
    .filter((heading): heading is string => heading !== null)
    .filter((heading) => headingState(heading).state === 'ambiguous');

  const openPopup = (heading: string, cell: Element) => {
    if (closeTimerRef.current !== null) {
      clearTimeout(closeTimerRef.current);
      closeTimerRef.current = null;
    }
    const rect = cell.getBoundingClientRect();
    setPopup({ heading, x: rect.right + 4, y: rect.top - 4 });
  };

  const scheduleClose = () => {
    if (closeTimerRef.current !== null) clearTimeout(closeTimerRef.current);
    closeTimerRef.current = setTimeout(() => setPopup(null), 200);
  };

  const cancelClose = () => {
    if (closeTimerRef.current !== null) {
      clearTimeout(closeTimerRef.current);
      closeTimerRef.current = null;
    }
  };

  const resolve = (heading: string, resolution: Resolution) => {
    setResolutions((previous) => ({ ...previous, [heading]: resolution }));
    setPopup(null);
  };

  const approve = () => {
    // Ship the user's identity decisions: matched headings append to their
    // target file, rejected ones force a new standalone file.
    const assignments: Record<string, string> = {};
    for (const line of extractedLines) {
      const heading = headingOf(line);
      if (!heading) continue;
      const { state, target } = headingState(heading);
      if (state === 'matched' && target) assignments[heading] = target;
      if (state === 'rejected') assignments[heading] = '';
    }
    onApprove(extracted, assignments);
  };

  const syncScroll = () => {
    const textarea = textareaRef.current;
    if (!textarea) return;
    if (backdropRef.current) {
      backdropRef.current.scrollTop = textarea.scrollTop;
      backdropRef.current.scrollLeft = textarea.scrollLeft;
    }
    if (gutterRef.current) {
      gutterRef.current.scrollTop = textarea.scrollTop;
    }
  };

  const popupCandidates = popup ? candidatesFor(popup.heading, registry) : [];
  const popupResolution = popup ? resolutions[popup.heading] : undefined;
  const popupState = popup ? headingState(popup.heading) : null;

  return (
    <section className="review-shell">
      <div className="review-header">
        <div>
          <p className="eyebrow">Review extraction</p>
          <p className="review-hint">
            Left: your original document. Right: the extracted measurements — edit freely, your
            version is what gets saved.{' '}
            {unverifiedClaims.length > 0
              ? `${unverifiedClaims.length} value${unverifiedClaims.length === 1 ? '' : 's'} (underlined in red) do not appear verbatim in the original — often unit conversions. `
              : ''}
            Green line numbers are metrics matched to an existing file (hover to change);{' '}
            yellow ones match several files and must be resolved before saving.
          </p>
        </div>
        <div className="actions">
          <button type="button" className="secondary-button" onClick={onCancel} disabled={busy}>
            Discard
          </button>
          <button
            type="button"
            className="primary-button"
            onClick={approve}
            disabled={busy || unresolved.length > 0}
            title={unresolved.length > 0 ? `Resolve ${unresolved.length} ambiguous metric name${unresolved.length === 1 ? '' : 's'} (yellow) first` : undefined}
          >
            {busy ? 'Saving…' : unresolved.length > 0 ? `Resolve ${unresolved.length} yellow` : 'Approve & save'}
          </button>
        </div>
      </div>

      {unitConflicts.length > 0 && (
        <div className="error-banner">
          Nothing was saved — unit mismatch with existing files. Convert these readings (or fix the
          unit) and approve again:
          <ul>
            {unitConflicts.map((conflict) => (
              <li key={`${conflict.line}-${conflict.text}`}>
                {conflict.metric}: “{conflict.text}” uses <strong>{conflict.unit}</strong>, but{' '}
                {conflict.target}.md is in <strong>{conflict.existingUnit}</strong>
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="review-panes">
        <div className="review-pane">
          <div className="review-scroll">
            <div className="review-gutter" aria-hidden="true">
              {originalDocLines.map((_, i) => (
                <div key={i}>{i + 1}</div>
              ))}
            </div>
            <pre className="review-original">{review.original}</pre>
          </div>
        </div>

        <div className="review-pane">
          <div className="review-gutter" ref={gutterRef}>
            {extractedLines.map((line, i) => {
              const heading = headingOf(line);
              const state = heading ? headingState(heading).state : null;
              const flagged = !heading && (unverifiedTexts.has(line.trim()) || conflictByText.has(line.trim()));
              const className = [
                state === 'matched' ? 'review-gutter-match' : '',
                state === 'ambiguous' ? 'review-gutter-ambiguous' : '',
                flagged ? 'review-gutter-flagged' : '',
              ]
                .filter(Boolean)
                .join(' ') || undefined;
              const interactive = heading && state !== 'none';
              return (
                <div
                  key={i}
                  className={className}
                  onMouseEnter={interactive ? (e: { currentTarget: Element }) => openPopup(heading, e.currentTarget) : undefined}
                  onMouseLeave={interactive ? scheduleClose : undefined}
                >
                  {i + 1}
                </div>
              );
            })}
          </div>
          <div className="review-editor">
            <pre className="review-backdrop" ref={backdropRef} aria-hidden="true">
              {extractedLines.map((line, i) => (
                <div key={i} className={!headingOf(line) && unverifiedTexts.has(line.trim()) ? 'review-line-flagged' : undefined}>
                  {renderLine(line)}
                </div>
              ))}
            </pre>
            <textarea
              className="review-extracted"
              ref={textareaRef}
              value={extracted}
              onChange={(e: ChangeEvent<HTMLTextAreaElement>) => setExtracted(e.target.value)}
              onScroll={syncScroll}
              spellCheck={false}
              wrap="off"
            />
          </div>
        </div>
      </div>

      {popup && popupState && (
        <div
          className="identity-popup"
          style={{ left: popup.x, top: popup.y }}
          onMouseEnter={cancelClose}
          onMouseLeave={scheduleClose}
        >
          <p className="identity-popup-title">{popup.heading}</p>
          {popupState.state === 'rejected' && <p className="identity-popup-note">Will be saved as a new file.</p>}
          {popupState.state === 'ambiguous' && <p className="identity-popup-note">Matches several files — pick one:</p>}
          {popupCandidates.map((id) => (
            <button
              key={id}
              type="button"
              className={`identity-popup-option${popupState.target === id ? ' identity-popup-current' : ''}`}
              onClick={() => resolve(popup.heading, { chosen: id })}
            >
              → {id}.md
            </button>
          ))}
          <button
            type="button"
            className={`identity-popup-option identity-popup-reject${popupResolution?.rejected ? ' identity-popup-current' : ''}`}
            onClick={() => resolve(popup.heading, { rejected: true })}
          >
            ✕ none — new file
          </button>
        </div>
      )}
    </section>
  );
}
