import { useEffect, useState, type ReactNode } from 'react';
import { Check, Copy, X } from 'lucide-react';
import { renderMarkdown } from '../lib/markdown';

/** Dialog that closes on Escape or a backdrop click. */
export function Modal(props: { title: ReactNode; onClose: () => void; children: ReactNode; footer?: ReactNode; wide?: boolean; icon?: ReactNode }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && props.onClose();
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [props]);
  return (
    <div className="overlay" onMouseDown={(e) => e.target === e.currentTarget && props.onClose()}>
      <div className={`modal ${props.wide ? 'wide' : ''}`} role="dialog" aria-modal="true">
        <div className="modal-head">
          {props.icon}
          <span className="grow">{props.title}</span>
          <button className="btn ghost icon sm" onClick={props.onClose} aria-label="Close">
            <X size={15} />
          </button>
        </div>
        <div className="modal-body">{props.children}</div>
        {props.footer && <div className="modal-foot">{props.footer}</div>}
      </div>
    </div>
  );
}

/** Accessible on/off toggle. */
export function Switch({ on, onChange, label }: { on: boolean; onChange: (v: boolean) => void; label?: string }) {
  return <button className={`switch ${on ? 'on' : ''}`} role="switch" aria-checked={on} aria-label={label} onClick={() => onChange(!on)} />;
}

/** Centered loading spinner with an optional label. */
export function Spinner({ label }: { label?: string }) {
  return (
    <div className="loading">
      <div className="col" style={{ alignItems: 'center' }}>
        <div className="spinner" />
        {label && <span>{label}</span>}
      </div>
    </div>
  );
}

/** Empty-state placeholder with an icon, title and body. */
export function Empty({ icon, title, children }: { icon: ReactNode; title: string; children?: ReactNode }) {
  return (
    <div className="empty">
      <div>
        <div className="hero-icon">{icon}</div>
        <h2>{title}</h2>
        <div style={{ maxWidth: 440, margin: '0 auto' }}>{children}</div>
      </div>
    </div>
  );
}

/** Renders an error message, or nothing when error is falsy. */
export function ErrorBox({ error }: { error: unknown }) {
  if (!error) return null;
  return <div className="error-box">{error instanceof Error ? error.message : String(error)}</div>;
}

/** Renders sanitized markdown. */
export function Markdown({ src, className = '' }: { src?: string; className?: string }) {
  return <div className={`md ${className}`} dangerouslySetInnerHTML={{ __html: renderMarkdown(src) }} />;
}

/** Copies text to the clipboard with brief confirmation. */
export function CopyButton({ text }: { text: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      className="btn sm copy"
      onClick={() => {
        navigator.clipboard?.writeText(text).catch(() => {});
        setDone(true);
        setTimeout(() => setDone(false), 1200);
      }}
    >
      {done ? <Check size={13} /> : <Copy size={13} />} {done ? 'Copied' : 'Copy'}
    </button>
  );
}

/** Monospace snippet with a copy button. */
export function Snippet({ text }: { text: string }) {
  return (
    <div className="snippet">
      {text}
      <CopyButton text={text} />
    </div>
  );
}

/** Runs an async loader, exposing data/error/loading; reruns on deps change. */
export function useAsync<T>(fn: () => Promise<T>, deps: unknown[]) {
  const [state, setState] = useState<{ data?: T; error?: unknown; loading: boolean }>({ loading: true });
  useEffect(() => {
    let alive = true;
    setState((s) => ({ data: s.data, loading: true }));
    fn().then(
      (data) => alive && setState({ data, loading: false }),
      (error) => alive && setState({ error, loading: false }),
    );
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
  return state;
}

/** Badge naming where a value came from: the extractor, the agent or the user. */
export function SourceBadge({ source }: { source: string }) {
  const label = source === 'auto' ? 'auto' : source === 'agent' ? 'agent' : 'you';
  const title = source === 'auto' ? 'Extracted automatically; may need checking' : source === 'agent' ? 'Recorded by the agent' : 'Entered by you';
  return (
    <span className={`src ${source}`} title={title}>
      {label}
    </span>
  );
}

/** Category badge with a color by family. */
export function CategoryBadge({ category, label }: { category: string; label?: string }) {
  if (!category) return null;
  const fam = /buck|boost|ldo|power|charger|load-switch/.test(category) ? 'power' : /mosfet|driver/.test(category) ? 'fet' : /mcu/.test(category) ? 'mcu' : '';
  return <span className={`cat ${fam}`}>{label ?? category}</span>;
}
