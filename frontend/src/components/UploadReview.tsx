import { useRef, useState, type ChangeEvent } from 'react';
import type { UploadReview as UploadReviewData } from '../api';

type UploadReviewProps = {
  review: UploadReviewData;
  busy: boolean;
  onApprove: (extracted: string) => void;
  onCancel: () => void;
};

// Side-by-side review of an upload: the untouched original on the left, the
// extracted canonical markdown on the right in an editable pane. Both panes
// have code-editor-style line numbers. Lines whose values the presence gate
// could not find in the original (unit conversions, repairs, fabrications)
// get a red underline; the marking is content-based, so editing a flagged
// line clears its underline. User edits are terminal — whatever is approved
// becomes ground truth.
export function UploadReview({ review, busy, onApprove, onCancel }: UploadReviewProps) {
  const [extracted, setExtracted] = useState(review.extracted);

  // The editable pane is a transparent-text textarea over a backdrop <pre>
  // that carries the visible text and the red underlines; scroll positions
  // are mirrored so they stay aligned.
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const backdropRef = useRef<HTMLPreElement | null>(null);
  const gutterRef = useRef<HTMLDivElement | null>(null);

  const unverifiedClaims = review.claims.filter((claim) => claim.verdict === 'unverified');

  // Flag lines by their content as originally extracted, not by line index:
  // indexes shift as soon as the user edits, and a corrected line should stop
  // being flagged anyway.
  const originalLines = review.extracted.split('\n');
  const unverifiedTexts = new Set(
    unverifiedClaims
      .map((claim) => originalLines[claim.line - 1]?.trim())
      .filter((text): text is string => Boolean(text)),
  );

  const extractedLines = extracted.split('\n');
  const originalDocLines = review.original.split('\n');

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

  return (
    <section className="review-shell">
      <div className="review-header">
        <div>
          <p className="eyebrow">Review extraction</p>
          <p className="review-hint">
            Left: your original document. Right: the extracted measurements — edit freely, your
            version is what gets saved. {unverifiedClaims.length > 0
              ? `${unverifiedClaims.length} value${unverifiedClaims.length === 1 ? '' : 's'} (underlined in red) do not appear verbatim in the original — often unit conversions. Check them before approving.`
              : 'Every extracted value appears verbatim in the original.'}
          </p>
        </div>
        <div className="actions">
          <button type="button" className="secondary-button" onClick={onCancel} disabled={busy}>
            Discard
          </button>
          <button type="button" className="primary-button" onClick={() => onApprove(extracted)} disabled={busy}>
            {busy ? 'Saving…' : 'Approve & save'}
          </button>
        </div>
      </div>

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
          <div className="review-gutter" aria-hidden="true" ref={gutterRef}>
            {extractedLines.map((line, i) => (
              <div key={i} className={unverifiedTexts.has(line.trim()) ? 'review-gutter-flagged' : undefined}>
                {i + 1}
              </div>
            ))}
          </div>
          <div className="review-editor">
            <pre className="review-backdrop" ref={backdropRef} aria-hidden="true">
              {extractedLines.map((line, i) => (
                <div key={i} className={unverifiedTexts.has(line.trim()) ? 'review-line-flagged' : undefined}>
                  {line || ' '}
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
    </section>
  );
}
