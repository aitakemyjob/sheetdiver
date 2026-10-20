import { useEffect } from 'react';
import { ChevronLeft, ChevronRight, Route, X } from 'lucide-react';
import { useStore } from '../lib/store';

/**
 * The agent's walkthrough: the step being shown, and buttons (or the
 * left/right arrow keys with Alt) to move through the steps. Each step's
 * explanation shows beside the view it opens.
 */
export function WalkthroughBar() {
  const w = useStore((s) => s.walkthrough);
  const walkTo = useStore((s) => s.walkTo);
  const end = useStore((s) => s.endWalkthrough);
  useEffect(() => {
    if (!w) return;
    const onKey = (e: KeyboardEvent) => {
      if (!e.altKey) return;
      if (e.key === 'ArrowRight') walkTo(w.index + 1);
      if (e.key === 'ArrowLeft') walkTo(w.index - 1);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [w, walkTo]);
  if (!w) return null;
  const step = w.steps[w.index];
  const last = w.index === w.steps.length - 1;
  return (
    <div className="walkthrough" role="region" aria-label="Walkthrough">
      <Route size={14} />
      <b>{w.title}</b>
      <span className="faint">
        Step {w.index + 1} of {w.steps.length}
      </span>
      <span className="grow walkthrough-step">{step?.title}</span>
      <button className="btn sm" disabled={w.index === 0} onClick={() => walkTo(w.index - 1)} title="Previous step (Alt+←)">
        <ChevronLeft size={13} /> Back
      </button>
      {last ? (
        <button className="btn sm primary" onClick={end}>
          Done
        </button>
      ) : (
        <button className="btn sm primary" onClick={() => walkTo(w.index + 1)} title="Next step (Alt+→)">
          Next <ChevronRight size={13} />
        </button>
      )}
      <button className="btn ghost icon sm" title="End the walkthrough" onClick={end}>
        <X size={13} />
      </button>
    </div>
  );
}
