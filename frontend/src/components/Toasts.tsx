import { AlertTriangle, Bot, CheckCircle2, Info, X } from 'lucide-react';
import { useStore } from '../lib/store';

/** Stack of transient notifications, some with an action button. */
export function Toasts() {
  const { toasts, dismissToast } = useStore();
  return (
    <div className="toasts" aria-live="polite">
      {toasts.map((t) => (
        <div key={t.id} className={`toast ${t.kind}`}>
          <span style={{ marginTop: 1, color: t.kind === 'agent' ? 'var(--accent-2)' : t.kind === 'ok' ? 'var(--ok)' : t.kind === 'err' ? 'var(--err)' : 'var(--accent)' }}>
            {t.kind === 'agent' ? <Bot size={16} /> : t.kind === 'ok' ? <CheckCircle2 size={16} /> : t.kind === 'err' || t.kind === 'warn' ? <AlertTriangle size={16} /> : <Info size={16} />}
          </span>
          <div className="grow">
            <div className="t-title">{t.title}</div>
            {t.body && <div className="t-body">{t.body}</div>}
            {t.action && (
              <button
                className="btn sm primary"
                style={{ marginTop: 8 }}
                onClick={() => {
                  t.action!.run();
                  dismissToast(t.id);
                }}
              >
                {t.action.label}
              </button>
            )}
          </div>
          <button className="btn ghost icon sm" onClick={() => dismissToast(t.id)} aria-label="Dismiss">
            <X size={13} />
          </button>
        </div>
      ))}
    </div>
  );
}
