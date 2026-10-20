import { useEffect } from 'react';
import { Star } from 'lucide-react';
import { api } from '../lib/api';
import { categories } from '../lib/format';
import { useStore } from '../lib/store';
import type { CompareColumn, CompareRow, CompareValue } from '../lib/types';
import { CategoryBadge, ErrorBox, Spinner, useAsync } from '../components/ui';
import { rangeText } from './CategoryView';

/**
 * The number a value is ranked by in a direction, as the backend compares
 * alternatives: a rating's max (then typ, min) where higher is better, the
 * typical (then max, min) where lower is better.
 */
function rank(v: CompareValue | undefined, dir: CompareColumn['direction']): number | undefined {
  if (!v) return undefined;
  if (dir === 'higher') return v.max ?? v.typ ?? v.min ?? undefined;
  if (dir === 'lower') return v.typ ?? v.max ?? v.min ?? undefined;
  return undefined;
}

/**
 * Marks each part's value in a row as best or worst, where the row's
 * direction says which values are better (higher or lower) and the parts
 * differ.
 */
export function marks(rows: CompareRow[], col: CompareColumn): Record<string, 'best' | 'worst'> {
  const vals = rows.map((r) => [r.part.mpn, rank(r.values[col.key], col.direction)] as const).filter((x): x is readonly [string, number] => x[1] !== undefined);
  if (vals.length < 2) return {};
  const nums = vals.map((x) => x[1]);
  const hi = Math.max(...nums);
  const lo = Math.min(...nums);
  if (hi === lo) return {};
  const [best, worst] = col.direction === 'higher' ? [hi, lo] : [lo, hi];
  const out: Record<string, 'best' | 'worst'> = {};
  for (const [mpn, n] of vals) {
    if (n === best) out[mpn] = 'best';
    else if (n === worst) out[mpn] = 'worst';
  }
  return out;
}

/**
 * Parts side by side, as the agent put them: one column per part, one
 * row per parameter with the best and worst marked, the agent's note on
 * each part and its recommendation highlighted. Values open their page.
 */
export function CompareView({ params }: { params: Record<string, unknown> }) {
  const s = useStore();
  const parts = (params.parts as string[] | undefined) ?? [];
  const keys = params.keys as string[] | undefined;
  const notes = (params.notes as Record<string, string> | undefined) ?? {};
  const pick = String(params.highlight ?? '').toUpperCase();
  const partList = parts.join(', ');
  const res = useAsync(() => api.compare({ parts, keys }), [partList, (keys ?? []).join(','), s.libraryVersion]);
  useEffect(() => {
    api.uiContext({ view: 'compare', selection: partList }).catch(() => {});
  }, [partList]);
  if (res.loading && !res.data) return <Spinner label={`Comparing ${parts.join(', ')}…`} />;
  if (res.error) return <ErrorBox error={res.error} />;
  const d = res.data;
  if (!d) return null;
  const open = (r: CompareRow, v: CompareValue) =>
    r.part.datasheetId && s.open({ kind: 'page', title: `${r.part.mpn} p${v.page + 1}`, params: { ds: r.part.datasheetId, page: v.page, box: v.box ?? undefined, part: r.part.mpn, nonce: Date.now() } });
  return (
    <div className="view">
      <div className="toolbar">
        <span className="title">{d.rows.map((r) => r.part.mpn).join(' vs ')}</span>
        <span className="faint">best and worst marked where higher or lower is better</span>
      </div>
      <div className="scroll pad" style={{ flex: 1 }}>
        <table className="compare-side">
          <thead>
            <tr>
              <th />
              {d.rows.map((r) => (
                <th key={r.part.id} className={r.part.mpn.toUpperCase() === pick ? 'picked' : ''}>
                  <button className="link" onClick={() => s.open({ kind: 'part', title: r.part.mpn, params: { part: r.part.mpn } })}>
                    {r.part.mpn}
                  </button>
                  {r.part.mpn.toUpperCase() === pick && (
                    <span className="badge ok" title="The agent's recommendation">
                      <Star size={10} /> recommended
                    </span>
                  )}
                  <div className="faint">
                    {r.part.manufacturer} {r.part.category && <CategoryBadge category={r.part.category} label={categories[r.part.category]} />}
                  </div>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {(d.columns ?? []).map((c) => {
              const m = marks(d.rows, c);
              return (
                <tr key={c.key}>
                  <th title={c.key}>
                    {c.label}
                    {c.unit && <span className="faint"> ({c.unit})</span>}
                  </th>
                  {d.rows.map((r) => {
                    const v = r.values[c.key];
                    return (
                      <td key={r.part.id} className={`${m[r.part.mpn] ?? ''} ${r.part.mpn.toUpperCase() === pick ? 'picked' : ''}`}>
                        {v ? (
                          <button className="link mono" title={`p${v.page + 1}${v.conditions ? ` · ${v.conditions}` : ''}`} onClick={() => open(r, v)}>
                            {rangeText(v)}
                          </button>
                        ) : (
                          <span className="faint">–</span>
                        )}
                        {m[r.part.mpn] && <span className={`mark ${m[r.part.mpn]}`}>{m[r.part.mpn]}</span>}
                      </td>
                    );
                  })}
                </tr>
              );
            })}
            {Object.keys(notes).length > 0 && (
              <tr className="notes">
                <th>Agent's notes</th>
                {d.rows.map((r) => (
                  <td key={r.part.id} className={r.part.mpn.toUpperCase() === pick ? 'picked' : ''}>
                    {notes[r.part.mpn] ?? Object.entries(notes).find(([k]) => k.toUpperCase() === r.part.mpn.toUpperCase())?.[1] ?? ''}
                  </td>
                ))}
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
