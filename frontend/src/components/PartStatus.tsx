import { Check, ChevronDown, Star } from 'lucide-react';
import { useStore } from '../lib/store';
import type { Part } from '../lib/types';
import { Menu } from './Menu';

/** Display names and badge styles of part statuses. */
export const statusInfo: Record<string, { label: string; tone: string }> = {
  preferred: { label: 'Preferred', tone: 'ok' },
  approved: { label: 'Approved', tone: 'accent' },
  avoid: { label: 'Avoid', tone: 'err' },
  nrnd: { label: 'NRND', tone: 'warn' },
  eol: { label: 'EOL', tone: 'err' },
};

/** Reports whether a status warns against using a part. */
export function discouraged(status?: string): boolean {
  return status === 'avoid' || status === 'nrnd' || status === 'eol';
}

/** A part's status as a badge (nothing when it has none). */
export function StatusBadge({ status }: { status?: string }) {
  const s = useStore();
  if (!status) return null;
  const info = statusInfo[status] ?? { label: status, tone: '' };
  return (
    <span className={`badge ${info.tone}`} title={s.app?.statuses?.[status]}>
      {info.label}
    </span>
  );
}

/** Star toggle for a part. */
export function FavoriteStar({ part, size = 14 }: { part: Part; size?: number }) {
  const s = useStore();
  return (
    <button
      className={`star ${part.favorite ? 'on' : ''}`}
      title={part.favorite ? 'Remove from favorites' : 'Add to favorites'}
      aria-pressed={part.favorite}
      onClick={(e) => {
        e.stopPropagation();
        s.setFavorite(part, !part.favorite);
      }}
    >
      <Star size={size} fill={part.favorite ? 'currentColor' : 'none'} />
    </button>
  );
}

/** Menu that sets a part's status. */
export function StatusPicker({ part }: { part: Part }) {
  const s = useStore();
  const statuses = s.app?.statuses ?? {};
  return (
    <Menu
      width={300}
      trigger={() => (
        <button className="btn sm" title="Your standing for this part: shown in comparisons and checked in projects">
          {part.status ? <StatusBadge status={part.status} /> : 'Set status'} <ChevronDown size={12} />
        </button>
      )}
      items={[
        ...Object.keys(statusInfo).map((k) => ({
          key: k,
          label: (
            <>
              <StatusBadge status={k} /> <span className="faint">{statuses[k]}</span>
            </>
          ),
          hint: part.status === k ? <Check size={13} /> : undefined,
          active: part.status === k,
          onSelect: () => s.setStatus(part, k),
        })),
        { key: 'sep', label: '', separator: true },
        { key: 'none', label: 'No status', hint: part.status ? undefined : <Check size={13} />, onSelect: () => s.setStatus(part, '') },
      ]}
    />
  );
}
