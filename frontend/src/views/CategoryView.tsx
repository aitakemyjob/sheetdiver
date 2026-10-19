import { useEffect, useMemo, useState } from 'react';
import { ArrowDown, ArrowUp, Columns3, Download, Star, X } from 'lucide-react';
import { api, saveText } from '../lib/api';
import { categories, eng, parseSI } from '../lib/format';
import { useStore } from '../lib/store';
import { FavoriteStar, StatusBadge, statusInfo } from '../components/PartStatus';
import type { CompareFilter, CompareResult, CompareValue } from '../lib/types';
import { Menu } from '../components/Menu';
import { ErrorBox, SourceBadge } from '../components/ui';

/** A column filter as typed: operator and text such as "24 V" or "1µ". */
interface FilterText {
  op: '>=' | '<=';
  text: string;
}

/** Formats a compared value: a range, a single typ, or min/typ/max. */
export function rangeText(v: CompareValue): string {
  const e = (x?: number | null) => (x === null || x === undefined ? '–' : eng(x, v.unit));
  const has = (x?: number | null) => x !== null && x !== undefined;
  if (has(v.min) && has(v.max) && !has(v.typ)) return `${e(v.min)} … ${e(v.max)}`;
  if (!has(v.min) && !has(v.max)) return e(v.typ);
  if (!has(v.typ) && has(v.max)) return `≤ ${e(v.max)}`;
  if (!has(v.typ) && has(v.min)) return `≥ ${e(v.min)}`;
  return `${e(v.min)} / ${e(v.typ)} / ${e(v.max)}`;
}

/** Turns typed filters into API filters, skipping ones that don't parse. */
export function toFilters(f: Record<string, FilterText>): CompareFilter[] {
  const out: CompareFilter[] = [];
  for (const [key, { op, text }] of Object.entries(f)) {
    if (!text.trim()) continue;
    const v = parseSI(text);
    if (v !== undefined && Number.isFinite(v)) out.push({ key, op, value: v });
  }
  return out;
}

/** CSV of the comparison as shown. */
function toCSV(r: CompareResult): string {
  const cols = r.columns ?? [];
  const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
  const lines = [['part', 'manufacturer', 'status', ...cols.flatMap((c) => [`${c.key} min`, `${c.key} typ`, `${c.key} max`, `${c.key} unit`])].join(',')];
  for (const row of r.rows) {
    const cells = [row.part.mpn, row.part.manufacturer, row.part.status ?? ''];
    for (const c of cols) {
      const v = row.values[c.key];
      cells.push(...[v?.min, v?.typ, v?.max].map((x) => (x === null || x === undefined ? '' : String(x))), v?.unit ?? '');
    }
    lines.push(cells.map((c) => esc(c ?? '')).join(','));
  }
  return lines.join('\n') + '\n';
}

/**
 * The parts of one component type side by side on their key parameters,
 * with sortable columns, per-column ≥/≤ filters, a column picker and text
 * search. Cells open the datasheet page the value comes from.
 */
export function CategoryView({ params }: { params: Record<string, unknown> }) {
  const s = useStore();
  const category = String(params.category ?? '');
  const highlight = String(params.highlight ?? '').toUpperCase();
  const [query, setQuery] = useState('');
  const [keys, setKeys] = useState<string[] | undefined>();
  const [filters, setFilters] = useState<Record<string, FilterText>>({});
  const [sort, setSort] = useState<{ key: string; desc: boolean }>({ key: 'mpn', desc: false });
  const [favorite, setFavorite] = useState(false);
  const [status, setStatus] = useState('');
  const [res, setRes] = useState<CompareResult>();
  const [error, setError] = useState<unknown>();
  const parsed = useMemo(() => toFilters(filters), [filters]);

  useEffect(() => {
    api.uiContext({ view: `category/${category}` }).catch(() => {});
  }, [category]);
  useEffect(() => {
    const t = setTimeout(() => {
      api
        .compare({ category, keys, filters: parsed, query, sort: sort.key, desc: sort.desc, favorite, status: status || undefined })
        .then((r) => {
          setRes(r);
          setError(undefined);
          if (!keys) setKeys((r.columns ?? []).map((c) => c.key));
        })
        .catch(setError);
    }, 200);
    return () => clearTimeout(t);
  }, [category, keys, parsed, query, sort, favorite, status, s.libraryVersion]);

  const allKeys = Object.keys(s.app?.keys ?? {}).sort();
  const cols = res?.columns ?? [];
  const label = categories[category] ?? (category || 'Uncategorized');
  const setFilter = (key: string, patch: Partial<FilterText>) => setFilters({ ...filters, [key]: { op: filters[key]?.op ?? '>=', text: filters[key]?.text ?? '', ...patch } });
  const sortBy = (key: string) => setSort(sort.key === key ? { key, desc: !sort.desc } : { key, desc: key !== 'mpn' && key !== 'iq' && !key.startsWith('rdson') && key !== 'isd' ? true : false });
  const download = async () => {
    if (!res) return;
    try {
      const where = await saveText(`${category || 'parts'}-comparison.csv`, toCSV(res), 'text/csv', !!s.app?.canSaveFile);
      if (where && s.app?.canSaveFile) s.toast({ kind: 'ok', title: 'Comparison saved', body: where });
    } catch (e) {
      s.toast({ kind: 'err', title: 'Could not save the CSV', body: (e as Error).message });
    }
  };
  const open = (mpn: string, ds: number, v: CompareValue) =>
    s.open({ kind: 'page', title: `${mpn} p${v.page + 1}`, params: { ds, page: v.page, box: v.box ?? undefined, part: mpn, nonce: Date.now() } });

  return (
    <div className="view">
      <div className="toolbar">
        <span className="title">{label}</span>
        {res && (
          <span className="faint">
            {res.rows.length} of {res.total} parts
          </span>
        )}
        <input className="input" style={{ width: 260 }} placeholder="Filter by part number, maker, description" value={query} onChange={(e) => setQuery(e.target.value)} />
        <button className={`btn sm ${favorite ? 'on' : ''}`} aria-pressed={favorite} title="Only starred parts" onClick={() => setFavorite(!favorite)}>
          <Star size={13} fill={favorite ? 'currentColor' : 'none'} style={favorite ? { color: 'var(--warn)' } : undefined} /> Favorites
        </button>
        <select className="input select" style={{ width: 130, height: 26 }} value={status} onChange={(e) => setStatus(e.target.value)} title="Only parts with this status">
          <option value="">Any status</option>
          {Object.entries(statusInfo).map(([k, v]) => (
            <option key={k} value={k}>
              {v.label}
            </option>
          ))}
        </select>
        <div className="grow" />
        {Object.values(filters).some((f) => f.text.trim()) && (
          <button className="btn sm ghost" onClick={() => setFilters({})}>
            <X size={13} /> Clear filters
          </button>
        )}
        <Menu
          align="right"
          width={300}
          trigger={() => (
            <button className="btn sm">
              <Columns3 size={13} /> Columns
            </button>
          )}
          items={allKeys.map((k) => ({
            key: k,
            label: k,
            hint: s.app?.keys[k],
            active: keys?.includes(k),
            onSelect: () => setKeys(keys?.includes(k) ? keys.filter((x) => x !== k) : [...(keys ?? []), k]),
          }))}
        />
        <button className="btn sm" onClick={download} disabled={!res} title="Download as CSV">
          <Download size={13} /> CSV
        </button>
      </div>
      <div className="scroll pad" style={{ flex: 1 }}>
        <ErrorBox error={error} />
        {res?.unknown && Object.keys(res.unknown).length > 0 && (
          <div className="muted" style={{ fontSize: 12, marginBottom: 8 }}>
            {Object.entries(res.unknown)
              .map(([k, n]) => `${n} ${n === 1 ? 'part has' : 'parts have'} no ${k} value`)
              .join(' · ')}{' '}
            (excluded by the filter; ask your agent to record the missing values).
          </div>
        )}
        <table className="dtable compare">
          <thead>
            <tr>
              <th className="sortable" onClick={() => sortBy('mpn')}>
                Part {sort.key === 'mpn' && (sort.desc ? <ArrowDown size={11} /> : <ArrowUp size={11} />)}
              </th>
              {cols.map((c) => (
                <th key={c.key} className="sortable" style={{ textAlign: 'right' }} title={`${c.label}\n${c.have} of ${res?.total ?? 0} parts have a value`} onClick={() => sortBy(c.key)}>
                  {c.key} {c.unit && <span className="faint">{c.unit}</span>} {sort.key === c.key && (sort.desc ? <ArrowDown size={11} /> : <ArrowUp size={11} />)}
                </th>
              ))}
            </tr>
            <tr className="filters">
              <th />
              {cols.map((c) => (
                <th key={c.key}>
                  <div className="row" style={{ gap: 2, justifyContent: 'flex-end' }}>
                    <select className="input select" style={{ width: 44, height: 24, padding: '0 14px 0 4px', fontSize: 11 }} value={filters[c.key]?.op ?? '>='} onChange={(e) => setFilter(c.key, { op: e.target.value as '>=' | '<=' })}>
                      <option value=">=">≥</option>
                      <option value="<=">≤</option>
                    </select>
                    <input
                      className="input"
                      style={{ width: 70, height: 24, fontSize: 11.5, textAlign: 'right' }}
                      placeholder={c.unit || 'value'}
                      value={filters[c.key]?.text ?? ''}
                      onChange={(e) => setFilter(c.key, { text: e.target.value })}
                    />
                  </div>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {res?.rows.map((row) => (
              <tr key={row.part.id} className={row.part.mpn.toUpperCase() === highlight ? 'hl' : ''}>
                <td>
                  <div className="row" style={{ gap: 6 }}>
                    <FavoriteStar part={row.part} size={12} />
                    <a style={{ cursor: 'pointer', fontFamily: 'var(--font-mono)', fontWeight: 600 }} onClick={() => s.open({ kind: 'part', title: row.part.mpn, params: { part: row.part.mpn } })}>
                      {row.part.mpn}
                    </a>
                    <StatusBadge status={row.part.status} />
                  </div>
                  <div className="faint ellipsis" style={{ fontSize: 11, maxWidth: 260 }} title={row.part.description}>
                    {row.part.manufacturer}
                  </div>
                </td>
                {cols.map((c) => {
                  const v = row.values[c.key];
                  return (
                    <td key={c.key} className="num" title={v ? `${v.conditions ?? ''}\npage ${v.page + 1}${v.rating ? '\nabsolute-maximum rating' : ''}` : 'not on the datasheet (or not extracted)'}>
                      {v ? (
                        <a style={{ cursor: 'pointer', color: 'var(--text)' }} onClick={() => open(row.part.mpn, row.part.datasheetId, v)}>
                          {rangeText(v)}
                          {v.rating && <span className="badge warn" style={{ marginLeft: 4 }}>max</span>}
                          {v.source !== 'auto' && (
                            <span style={{ marginLeft: 4 }}>
                              <SourceBadge source={v.source} />
                            </span>
                          )}
                        </a>
                      ) : (
                        <span className="faint">–</span>
                      )}
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
        <div className="faint" style={{ fontSize: 11.5, marginTop: 10 }}>
          ≥ tests the highest a part can do (its max, else typ); ≤ tests its typ, else max. Units and prefixes work: 24 V, 3 A, 650k, 1µ, 30m. Values are each part's key parameters, so
          corrections you or your agent record show up here.
        </div>
      </div>
    </div>
  );
}
