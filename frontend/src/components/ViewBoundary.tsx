import { Component, type ReactNode } from 'react';
import { RotateCcw, TriangleAlert } from 'lucide-react';

/**
 * Catches a crash in one tab's view, so the rest of the app keeps
 * working: the tab shows the error and can try again.
 */
export class ViewBoundary extends Component<{ children: ReactNode }, { error?: Error }> {
  state: { error?: Error } = {};

  /** Records the error to show instead of the view. */
  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  /** Logs the error for the console and bug reports. */
  componentDidCatch(error: Error) {
    console.error('[SheetDiver] view crashed:', error);
  }

  render() {
    const { error } = this.state;
    if (!error) return this.props.children;
    return (
      <div className="pad col" style={{ maxWidth: 640 }}>
        <h3 className="row" style={{ gap: 8 }}>
          <TriangleAlert size={16} style={{ color: 'var(--err)' }} /> This view ran into a problem
        </h3>
        <div className="error-box">{error.message}</div>
        <div className="faint">The rest of SheetDiver still works; other tabs are unaffected.</div>
        <div>
          <button className="btn sm" onClick={() => this.setState({ error: undefined })}>
            <RotateCcw size={13} /> Try again
          </button>
        </div>
      </div>
    );
  }
}
