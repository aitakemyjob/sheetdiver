import { useState } from 'react';
import { ArrowDown, ArrowUp, Equal, HelpCircle, Shuffle } from 'lucide-react';
import { api } from '../lib/api';
import { useStore } from '../lib/store';
import type { AltCheck, PartDetail, Verdict } from '../lib/types';
import { FavoriteStar, StatusBadge } from '../components/PartStatus';
import { Empty, ErrorBox, Spinner, Switch, useAsync } from '../components/ui';

/** Icon and colour of a verdict. */
export const verdictInfo: Record<Verdict, { label: string; color: string }> = {
  better: { label: 'better', color: 'var(--ok)' },
  same: { label: 'same', color: 'var(--text-2)' },
  worse: { label: 'worse', color: 'var(--err)' },
  unknown: { label: 'unknown', color: 'var(--text-3)' },
};

/** Explains how a key is judged. */
export function directionHint(c: Pick<AltCheck, 'direction'>): string {
  return {
    higher: 'higher is better',
    lower: 'lower is better',
    cover: 'the range must cover the original’s',
    close: 'should match (within 10%)',
  }[c.direction];
}

/** Verdict icon. */
function VerdictIcon({ v }: { v: Verdict }) {
  const style = { color: verdictInfo[v].color, flex: 'none' as const };
  switch (v) {
    case 'better':
      return <ArrowUp size={12} style={style} />;
    case 'worse':
      return <ArrowDown size={12} style={style} />;
    case 'same':
      return <Equal size={12} style={style} />;
    default:
      return <HelpCircle size={12} style={style} />;
  }
}

/**
 * Parts in the library that could replace this one, compared key by key:
 * drop-ins (nothing worse) first, then the fewest worse keys.
 */
export function Alternatives({ d }: { d: PartDetail }) {
  const s = useStore();
  const [discouraged, setDiscouraged] = useState(false);
  const alt = useAsync(() => api.alternatives({ part: d.part.mpn, discouraged, limit: 30 }), [d.part.mpn, discouraged, s.libraryVersion]);
  const r = alt.data;
  const open = (mpn: string, ds: number, c: AltCheck) =>
    c.page >= 0 && s.open({ kind: 'page', title: `${mpn} p${c.page + 1}`, params: { ds, page: c.page, part: mpn, nonce: Date.now() } });
  return (
    <div className="pad col">
      <div className="row" style={{ flexWrap: 'wrap' }}>
        <span className="muted grow">
          {r ? (
            <>
              Library parts that could replace <b>{d.part.mpn}</b>, compared on {r.keys.join(', ')}. {r.considered} candidates considered.
            </>
          ) : (
            'Library parts that could replace this one.'
          )}
        </span>
        <label className="row" style={{ gap: 8 }}>
          <Switch on={discouraged} onChange={setDiscouraged} label="Include avoid, NRND and EOL parts" /> Include avoid / NRND / EOL
        </label>
      </div>
      {alt.loading && !r && <Spinner label="Comparing…" />}
      <ErrorBox error={alt.error} />
      {r && r.alternatives.length === 0 && (
        <Empty icon={<Shuffle size={26} />} title="No alternatives in the library">
          Import more {d.part.category || 'parts'} of this type (Import → Part numbers) and they'll be compared here.
        </Empty>
      )}
      {r && r.alternatives.length > 0 && (
        <table className="dtable compare">
          <thead>
            <tr>
              <th>Part</th>
              <th>Fit</th>
              {r.keys.map((k, i) => (
                <th key={k} title={directionHint(r.alternatives[0].checks[i])}>
                  {k}
                  <div className="faint mono" style={{ fontWeight: 400, fontSize: 10.5 }}>
                    {r.alternatives[0].checks[i]?.original}
                  </div>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {r.alternatives.map((a) => (
              <tr key={a.part.id}>
                <td>
                  <div className="row" style={{ gap: 6 }}>
                    <FavoriteStar part={a.part} size={12} />
                    <a style={{ cursor: 'pointer', fontFamily: 'var(--font-mono)', fontWeight: 600 }} onClick={() => s.open({ kind: 'part', title: a.part.mpn, params: { part: a.part.mpn } })}>
                      {a.part.mpn}
                    </a>
                    <StatusBadge status={a.part.status} />
                  </div>
                  <div className="faint" style={{ fontSize: 11 }}>
                    {[a.part.manufacturer, a.sameDatasheet && 'same datasheet', a.samePackage && 'same package', a.part.category !== d.part.category && a.part.category].filter(Boolean).join(' · ')}
                  </div>
                </td>
                <td>
                  {a.dropIn ? (
                    <span className="badge ok" title={a.unknown ? `${a.unknown} key unknown` : 'Nothing worse'}>
                      drop-in
                    </span>
                  ) : (
                    <span className="badge warn">{a.worse} worse</span>
                  )}
                  {a.unknown > 0 && <div className="faint" style={{ fontSize: 11 }}>{a.unknown} unknown</div>}
                </td>
                {a.checks.map((c) => (
                  <td key={c.key} className="mono" style={{ fontSize: 12 }} title={`${verdictInfo[c.verdict].label}: ${directionHint(c)}`}>
                    <a className="row" style={{ gap: 4, cursor: c.page >= 0 ? 'pointer' : 'default', color: 'var(--text)' }} onClick={() => open(a.part.mpn, a.part.datasheetId, c)}>
                      <VerdictIcon v={c.verdict} /> {c.candidate}
                    </a>
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
