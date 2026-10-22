import { AlertOctagon, AlertTriangle, CheckCircle2 } from 'lucide-react';
import type { Check } from '../lib/types';

/** Icon for a check level. */
export function CheckIcon({ level }: { level: string }) {
  if (level === 'error') return <AlertOctagon size={14} style={{ color: 'var(--err)' }} />;
  if (level === 'warn') return <AlertTriangle size={14} style={{ color: 'var(--warn)' }} />;
  return <CheckCircle2 size={14} style={{ color: 'var(--ok)' }} />;
}

/** A list of checks. */
export function Checks({ checks }: { checks: Check[] }) {
  return (
    <div className="col" style={{ gap: 4 }}>
      {checks.map((c, i) => (
        <div key={i} className={`check ${c.level}`}>
          <CheckIcon level={c.level} />
          <span>{c.message}</span>
        </div>
      ))}
    </div>
  );
}

