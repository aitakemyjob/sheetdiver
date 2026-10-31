import { useEffect, useMemo, useRef, useState } from 'react';
import { ChevronDown, ChevronRight, Database, Download, ExternalLink, FileDown, Filter, Plus, RefreshCw, Search, X } from 'lucide-react';
import { api, openShop } from '../lib/api';
import { catalogProgressText, useStore } from '../lib/store';
import type { CatalogCategory, CatalogFilter, CatalogItem, CatalogParam } from '../lib/types';
import { libraryTone, priceText } from '../components/CatalogPanel';
import { Empty, ErrorBox, Spinner, Switch, useAsync } from '../components/ui';
import { Menu } from '../components/Menu';
import { eng } from '../lib/format';

/** Rows per page. */
const pageSize = 100;

/** Groups subcategories under their category, keeping the server's order. */
export function groupCategories(cats: CatalogCategory[], filter: string, inStockOnly: boolean) {
  const f = filter.trim().toLowerCase();
  const groups: { category: string; inStock: number; subs: CatalogCategory[] }[] = [];
  const at: Record<string, number> = {};
  for (const c of cats) {
    if (inStockOnly && c.inStock === 0) continue;
    if (f && !c.subcategory.toLowerCase().includes(f) && !c.category.toLowerCase().includes(f)) continue;
    const name = c.category || 'Other';
    if (at[name] === undefined) {
      at[name] = groups.length;
      groups.push({ category: name, inStock: 0, subs: [] });
    }
    groups[at[name]].subs.push(c);
    groups[at[name]].inStock += c.inStock;
  }
  return groups;
}

/** The filters that are complete enough to send. */
export function activeFilters(fs: CatalogFilter[]): CatalogFilter[] {
  // Text filters pick values; number filters (≥, ≤ or =) are typed.
  return fs.filter((f) => (f.values !== undefined ? f.values.length > 0 : f.input.trim() !== ''));
}

/** Summarizes a text filter's picked values for its chip. */
export function valuesLabel(values: string[] | undefined): string {
  if (!values?.length) return 'any';
  if (values.length <= 2) return values.join(', ');
  return `${values[0]} +${values.length - 1}`;
}

/** Returns value, updated only after it stops changing for ms. */
function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

/**
 * Multi-select for a text parameter (Package, Type…): its values with
 * counts under the other filters, a search box and checkboxes.
 */
function ValuePicker({ param, picked, onChange }: { param?: CatalogParam; picked: string[]; onChange: (v: string[]) => void }) {
  const [open, setOpen] = useState(picked.length === 0);
  const [q, setQ] = useState('');
  const ref = useRef<HTMLSpanElement>(null);
  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => ref.current && !ref.current.contains(e.target as Node) && setOpen(false);
    document.addEventListener('mousedown', close);
    return () => document.removeEventListener('mousedown', close);
  }, [open]);
  const counts = new Map((param?.values ?? []).map((v) => [v.value, v.count]));
  // Picked values stay listed even when the other filters leave none of them.
  const all = [...picked.filter((v) => !counts.has(v)), ...(param?.values ?? []).map((v) => v.value)];
  const shown = all.filter((v) => v.toLowerCase().includes(q.trim().toLowerCase()));
  const toggle = (v: string) => onChange(picked.includes(v) ? picked.filter((x) => x !== v) : [...picked, v]);
  return (
    <span className="value-picker" ref={ref}>
      <button className="btn sm" onClick={() => setOpen(!open)} title={picked.join(', ') || 'Pick values'}>
        {valuesLabel(picked)} <ChevronDown size={11} />
      </button>
      {open && (
        <div className="value-pop">
          <input className="input" autoFocus placeholder={`Search ${param?.key ?? 'values'}`} value={q} onChange={(e) => setQ(e.target.value)} />
          <div className="value-list">
            {shown.map((v) => (
              <label key={v} className={`value-row ${counts.get(v) ? '' : 'faint'}`}>
                <input type="checkbox" checked={picked.includes(v)} onChange={() => toggle(v)} />
                <span className="grow ellipsis" title={v}>
                  {v}
                </span>
                <span className="faint">{(counts.get(v) ?? 0).toLocaleString()}</span>
              </label>
            ))}
            {shown.length === 0 && <div className="faint" style={{ padding: 6 }}>No values match.</div>}
          </div>
          {picked.length > 0 && (
            <button className="btn sm ghost" onClick={() => onChange([])}>
              Clear {picked.length}
            </button>
          )}
        </div>
      )}
    </span>
  );
}

/** Columns for a category: filtered and sorted parameters, then its most common numeric ones (5 at most). */
export function paramColumns(params: CatalogParam[], filters: CatalogFilter[], sortKey: string): string[] {
  const cols: string[] = [];
  // Package has its own column; it isn't an attribute.
  const add = (k: string) => k && k !== 'Package' && !cols.includes(k) && cols.length < 5 && cols.push(k);
  filters.forEach((f) => add(f.key));
  if (sortKey.startsWith('attr:')) add(sortKey.slice(5));
  params.filter((p) => p.kind === 'number' && p.key !== 'Operating Temperature').forEach((p) => cols.length < 4 && add(p.key));
  return cols;
}

/** Describes a numeric parameter's range for a filter hint. */
export function paramRange(p: CatalogParam): string {
  if (p.kind !== 'number' || p.min === undefined || p.max === undefined) return '';
  return `${eng(p.min, p.unit ?? '')} … ${eng(p.max, p.unit ?? '')}`;
}

/**
 * The JLCPCB parts catalogue: browse by category or search, filter by
 * stock and JLC library, sort, page through results, inspect a part and
 * import the datasheets of the chosen ones.
 */
export function CatalogView({ params }: { params: Record<string, unknown> }) {
  const s = useStore();
  const c = s.catalog;
  const [text, setText] = useState(String(params.query ?? ''));
  const [sub, setSub] = useState<string>(String(params.subcategory ?? ''));
  // The subcategory's category: JLCPCB has subcategories of the same name
  // in several (MOSFETs under Transistors/Thyristors and Triode/MOS Tube/Transistor).
  const [cat, setCat] = useState<string>(String(params.category ?? ''));
  const choose = (category: string, subcategory: string) => {
    setCat(category);
    setSub(subcategory);
  };
  const [inStock, setInStock] = useState(true);
  const [basic, setBasic] = useState(false);
  const [sort, setSort] = useState<{ key: string; desc: boolean }>({ key: '', desc: false });
  const [page, setPage] = useState(0);
  const [items, setItems] = useState<CatalogItem[]>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<unknown>();
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [detail, setDetail] = useState<CatalogItem>();
  const [catFilter, setCatFilter] = useState('');
  const [open, setOpen] = useState<Set<string>>(new Set());
  const [filters, setFilters] = useState<CatalogFilter[]>((params.filters as CatalogFilter[] | undefined) ?? []);
  // The agent's candidates (LCSC codes) are shown instead of a search
  // until the user searches; its pick is highlighted.
  const agentCodes = useMemo(() => (params.lcsc as string[] | undefined) ?? [], [params.lcsc]);
  const [candidates, setCandidates] = useState(agentCodes.length > 0);
  const pick = String(params.highlight ?? '').toUpperCase();
  const firstSub = useRef(true);
  useEffect(() => {
    if (params.nonce === undefined) return;
    setText(String(params.query ?? ''));
    choose(String(params.category ?? ''), String(params.subcategory ?? ''));
    setFilters((params.filters as CatalogFilter[] | undefined) ?? []);
    setCandidates(agentCodes.length > 0);
    firstSub.current = true;
  }, [params.nonce, params.query, params.category, params.subcategory, params.filters, agentCodes.length]);
  const sent = useMemo(() => activeFilters(filters), [filters]);
  // Each parameter's values and range follow the other filters (faceted);
  // refetched once typing settles.
  const facetFilters = useDebounced(sent, 300);
  const catParams = useAsync(
    () => (c?.installed && sub ? api.catalogParams({ text: '', category: cat || undefined, subcategory: sub, inStock, basic, attrs: facetFilters.length ? facetFilters : undefined }) : Promise.resolve([] as CatalogParam[])),
    [c?.installed, cat, sub, inStock, basic, c?.downloaded, facetFilters],
  );
  const cols = useMemo(() => paramColumns(catParams.data ?? [], sent, sort.key), [catParams.data, sent, sort.key]);
  const cats = useAsync(() => (c?.installed ? api.catalogCategories() : Promise.resolve([])), [c?.installed, c?.downloaded]);
  const groups = useMemo(() => groupCategories(cats.data ?? [], catFilter, inStock), [cats.data, catFilter, inStock]);

  useEffect(() => {
    api.uiContext({ view: 'catalog', selection: sub || text }).catch(() => {});
  }, [sub, text]);
  useEffect(() => setPage(0), [text, cat, sub, inStock, basic, sort, sent]);
  // Parameters belong to a category: switching category drops its filters
  // (not the agent's, given with its category).
  useEffect(() => {
    if (firstSub.current) {
      firstSub.current = false;
      return;
    }
    setFilters([]);
    setSort((x) => (x.key.startsWith('attr:') ? { key: '', desc: false } : x));
  }, [cat, sub]);
  useEffect(() => {
    if (!c?.installed) return;
    if (candidates) {
      setLoading(true);
      api
        .catalogGet(agentCodes)
        .then((r) => {
          setItems(r);
          setError(undefined);
        }, setError)
        .finally(() => setLoading(false));
      return;
    }
    if (!text.trim() && !sub) {
      setItems(undefined);
      return;
    }
    const t = setTimeout(() => {
      setLoading(true);
      api
        .catalogSearch({ text, category: (sub && cat) || undefined, subcategory: sub || undefined, inStock, basic, attrs: sent.length ? sent : undefined, sort: sort.key || undefined, desc: sort.desc, limit: pageSize, offset: page * pageSize })
        .then((r) => {
          setItems(r);
          setError(undefined);
        }, setError)
        .finally(() => setLoading(false));
    }, 250);
    return () => clearTimeout(t);
  }, [text, cat, sub, inStock, basic, sent, sort, page, c?.installed, c?.downloaded, s.libraryVersion, candidates, agentCodes]);

  if (!c?.installed) {
    return (
      <div className="pad">
        <Empty icon={<Database size={28} />} title="Parts catalogue">
          <div style={{ maxWidth: 520 }}>
            Browse and search JLCPCB's catalogue of about 2.5 million parts (stock, price, assembly library, specs) and import the datasheets of the ones you want. It's downloaded once (about 200 MB) and
            searched offline.
          </div>
          <div style={{ marginTop: 12 }}>
            {c?.updating ? (
              <div style={{ width: 320 }}>
                <div className="faint">{catalogProgressText(c)}</div>
                <div className="bar">
                  <i style={{ width: `${c.progress?.total ? (100 * c.progress.done) / c.progress.total : 5}%` }} />
                </div>
              </div>
            ) : (
              <button className="btn primary" onClick={() => s.downloadCatalog()}>
                <Download size={13} /> Download catalogue
              </button>
            )}
          </div>
        </Empty>
      </div>
    );
  }

  const toggle = (lcsc: string) => {
    const n = new Set(picked);
    if (n.has(lcsc)) n.delete(lcsc);
    else n.add(lcsc);
    setPicked(n);
  };
  const importParts = async (codes: string[]) => {
    try {
      const jobs = await api.catalogImport(codes);
      s.setQueueOpen(true);
      s.toast({ kind: 'info', title: `Importing ${jobs.length} datasheet${jobs.length === 1 ? '' : 's'}` });
      setPicked(new Set());
    } catch (e) {
      s.toast({ kind: 'err', title: 'Could not import', body: (e as Error).message });
    }
  };
  const openShopOrToast = (lcsc: string, shop: 'lcsc' | 'jlcpcb') =>
    openShop(lcsc, shop, !!s.app?.canOpenFile).catch((e) => s.toast({ kind: 'err', title: `Could not open ${shop === 'lcsc' ? 'LCSC' : 'JLCPCB'}`, body: (e as Error).message }));
  // The details pane shows the description; the table keeps stock and price in view.
  const showDesc = !detail || cols.length === 0;
  const sortBy = (key: string) => setSort(sort.key === key ? { key, desc: !sort.desc } : { key, desc: false });
  const arrow = (key: string) => (sort.key === key ? (sort.desc ? ' ↓' : ' ↑') : '');

  return (
    <div className="catalog">
      <aside className="catalog-cats">
        <div className="catalog-cats-head">
          <input className="input" placeholder="Filter categories" value={catFilter} onChange={(e) => setCatFilter(e.target.value)} />
        </div>
        <div className="panel-body">
          {cats.loading && !cats.data && <Spinner label="Reading categories…" />}
          <ErrorBox error={cats.error} />
          {sub && (
            <button className="cat-group" onClick={() => setSub('')}>
              <X size={12} /> All categories
            </button>
          )}
          {groups.map((g) => {
            const expanded = open.has(g.category) || !!catFilter;
            return (
              <div key={g.category}>
                <button
                  className="cat-group"
                  onClick={() => {
                    const n = new Set(open);
                    if (n.has(g.category)) n.delete(g.category);
                    else n.add(g.category);
                    setOpen(n);
                  }}
                >
                  {expanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
                  <span className="grow ellipsis">{g.category}</span>
                  <span className="faint">{g.inStock.toLocaleString()}</span>
                </button>
                {expanded &&
                  g.subs.map((x) => {
                    // Picked by category and subcategory: another category's
                    // subcategory of the same name isn't this one. One the
                    // agent named without its category matches by name.
                    const on = sub === x.subcategory && (!cat || cat === x.category);
                    return (
                      <button key={x.category + x.subcategory} className={`cat-sub ${on ? 'on' : ''}`} aria-pressed={on} onClick={() => (on ? choose('', '') : choose(x.category, x.subcategory))} title={`${x.parts.toLocaleString()} parts, ${x.inStock.toLocaleString()} in stock`}>
                        <span className="grow ellipsis">{x.subcategory}</span>
                        <span className="faint">{(inStock ? x.inStock : x.parts).toLocaleString()}</span>
                      </button>
                    );
                  })}
              </div>
            );
          })}
        </div>
      </aside>
      <div className="catalog-main">
        <div className="toolbar" style={{ flexWrap: 'wrap' }}>
          <Search size={14} className="faint" />
          <input className="input" style={{ width: 340 }} autoFocus placeholder="Part number or specs: TPS563200 · buck 3A SOT-23 · 10k 0402" value={text} onChange={(e) => setText(e.target.value)} />
          {sub && (
            <span className="chip on" onClick={() => choose('', '')} title="Clear the category">
              {cat ? `${cat} › ${sub}` : sub} <X size={11} />
            </span>
          )}
          <label className="row" style={{ gap: 6 }}>
            <Switch on={inStock} onChange={setInStock} label="In stock only" /> In stock
          </label>
          <label className="row" style={{ gap: 6 }} title="JLCPCB basic and preferred parts have no extended-part setup fee">
            <Switch on={basic} onChange={setBasic} label="Basic parts only" /> Basic / preferred
          </label>
          <div className="grow" />
          <span className={`badge ${c.stale ? 'warn' : ''}`} title={`Published ${new Date(c.published).toLocaleString()}`}>
            {c.ageDays === 0 ? 'today' : `${c.ageDays} days old`}
          </span>
          <button className="btn sm ghost" disabled={c.updating} onClick={() => s.downloadCatalog()} title="Download the latest catalogue">
            <RefreshCw size={12} /> {c.updating ? catalogProgressText(c) : 'Refresh'}
          </button>
          <button className="btn sm primary" disabled={!picked.size} onClick={() => importParts([...picked])}>
            <FileDown size={13} /> Import {picked.size || ''} datasheet{picked.size === 1 ? '' : 's'}
          </button>
        </div>
        {sub && (
          <div className="toolbar catalog-filters" style={{ flexWrap: 'wrap' }}>
            <Filter size={13} className="faint" />
            {filters.map((f, i) => {
              const p = catParams.data?.find((x) => x.key === f.key);
              const set = (patch: Partial<CatalogFilter>) => setFilters(filters.map((x, j) => (j === i ? { ...x, ...patch } : x)));
              return (
                <span key={f.key + i} className="filter-chip">
                  <b>{f.key}</b>
                  {f.values !== undefined ? (
                    <ValuePicker param={p} picked={f.values} onChange={(values) => set({ values })} />
                  ) : (
                    <>
                      <select className="input select" value={f.op} onChange={(e) => set({ op: e.target.value as CatalogFilter['op'] })} title="≥ at least, ≤ at most, = exactly (or a range containing it)">
                        <option value=">=">≥</option>
                        <option value="<=">≤</option>
                        <option value="=">=</option>
                      </select>
                      <input
                        className="input"
                        style={{ width: 96 }}
                        autoFocus={!f.input}
                        list={f.op === '=' ? `values-${i}` : undefined}
                        placeholder={f.op === '=' ? 'e.g. 10k|4.7k' : p?.unit ? `e.g. 10${p.unit}` : 'value'}
                        title={p ? `Range in this category: ${paramRange(p)}${f.op === '=' ? '. Several values: separate with |' : ''}` : ''}
                        value={f.input}
                        onChange={(e) => set({ input: e.target.value })}
                      />
                      {f.op === '=' && (
                        <datalist id={`values-${i}`}>
                          {p?.values?.map((v) => (
                            <option key={v.value} value={v.value}>
                              {v.count.toLocaleString()} parts
                            </option>
                          ))}
                        </datalist>
                      )}
                    </>
                  )}
                  <button className="btn ghost icon sm" onClick={() => setFilters(filters.filter((_, j) => j !== i))} aria-label={`Remove the ${f.key} filter`}>
                    <X size={11} />
                  </button>
                </span>
              );
            })}
            <Menu
              width={340}
              trigger={() => (
                <button className="btn sm" disabled={!catParams.data?.length}>
                  <Plus size={12} /> {catParams.loading ? 'Reading parameters…' : 'Filter on a parameter'}
                </button>
              )}
              items={(catParams.data ?? []).map((p) => ({
                key: p.key,
                label: p.key,
                hint: p.kind === 'number' ? paramRange(p) : `${p.values?.length ?? 0} values`,
                onSelect: () => setFilters([...filters, p.kind === 'text' ? { key: p.key, op: '=', input: '', values: [] } : { key: p.key, op: '>=', input: '' }]),
              }))}
            />
            {filters.length > 0 && (
              <button className="btn sm ghost" onClick={() => setFilters([])}>
                Clear
              </button>
            )}
          </div>
        )}
        <div className="catalog-body">
          <div className="scroll" style={{ flex: 1 }}>
            <ErrorBox error={error} />
            {!items && !loading && (
              <Empty icon={<Database size={26} />} title={`${c.parts.toLocaleString()} parts · ${c.inStock.toLocaleString()} in stock`}>
                Pick a category on the left or search by part number or specs.
              </Empty>
            )}
            {loading && !items && <Spinner label="Searching…" />}
            {items && items.length === 0 && <div className="faint pad">No parts match.</div>}
            {candidates && (
              <div className="row agent-candidates">
                <b>Your agent's candidates</b>
                <span className="faint">{agentCodes.length} parts</span>
                <div className="grow" />
                <button className="btn sm" onClick={() => setCandidates(false)}>
                  Search the catalogue instead
                </button>
              </div>
            )}
            {items && items.length > 0 && (
              <table className="dtable catalog-table">
                <thead>
                  <tr>
                    <th style={{ width: 26 }} />
                    <th className="sortable part-col" onClick={() => sortBy('mpn')}>
                      Part{arrow('mpn')}
                    </th>
                    {cols.map((k) => (
                      <th key={k} className="sortable param-col" title={`Sort by ${k}`} onClick={() => sortBy(`attr:${k}`)}>
                        {k}
                        {arrow(`attr:${k}`)}
                      </th>
                    ))}
                    {showDesc && <th>Description</th>}
                    <th>Package</th>
                    <th>JLC</th>
                    <th className="sortable num-col" onClick={() => sortBy('stock')}>
                      Stock{arrow('stock')}
                    </th>
                    <th className="sortable num-col" onClick={() => sortBy('price')}>
                      Price{arrow('price')}
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((it) => (
                    <tr
                      key={it.lcsc}
                      className={`${detail?.lcsc === it.lcsc ? 'hl' : ''} ${it.lcsc.toUpperCase() === pick ? 'agent-hl' : ''}`}
                      title={it.lcsc.toUpperCase() === pick ? "The agent's recommendation" : undefined}
                      style={{ cursor: 'pointer' }}
                      onClick={() => setDetail(it)}
                    >
                      <td onClick={(e) => e.stopPropagation()}>
                        <input type="checkbox" checked={picked.has(it.lcsc)} onChange={() => toggle(it.lcsc)} aria-label={`Select ${it.mpn}`} />
                      </td>
                      <td className="part-col">
                        <div className="row" style={{ gap: 6 }}>
                          <span className="mono" style={{ fontWeight: 600 }}>
                            {it.mpn}
                          </span>
                          {it.inLibrary && <span className="badge ok">in library</span>}
                        </div>
                        <div className="maker" title={it.manufacturer}>
                          <a
                            className="shop-link"
                            title={`Open ${it.lcsc} at LCSC`}
                            onClick={(e) => {
                              e.stopPropagation();
                              openShopOrToast(it.lcsc, 'lcsc');
                            }}
                          >
                            {it.lcsc}
                          </a>{' '}
                          · {it.manufacturer || 'unknown maker'}
                        </div>
                      </td>
                      {cols.map((k) => (
                        <td key={k} className="param-col" title={it.attributes?.[k]}>
                          {it.attributes?.[k] ?? '–'}
                        </td>
                      ))}
                      {showDesc && (
                        <td className="desc-col" style={{ maxWidth: cols.length ? 220 : 380 }} title={it.description}>
                          {it.description}
                        </td>
                      )}
                      <td className="mono" style={{ fontSize: 12 }}>
                        {it.package}
                      </td>
                      <td>
                        <span className={`badge ${libraryTone[it.library]}`}>{it.library}</span>
                      </td>
                      <td className="num">{it.stock.toLocaleString()}</td>
                      <td className="num">{priceText(it.price)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
            {items && (page > 0 || items.length === pageSize) && (
              <div className="row pad" style={{ justifyContent: 'center' }}>
                <button className="btn sm" disabled={page === 0 || loading} onClick={() => setPage(page - 1)}>
                  Previous
                </button>
                <span className="faint">
                  {page * pageSize + 1}–{page * pageSize + items.length}
                </span>
                <button className="btn sm" disabled={items.length < pageSize || loading} onClick={() => setPage(page + 1)}>
                  Next
                </button>
              </div>
            )}
          </div>
          {detail && <CatalogDetail it={detail} onClose={() => setDetail(undefined)} onImport={() => importParts([detail.lcsc])} />}
        </div>
      </div>
    </div>
  );
}

/** A catalogue part's details: attributes, links and import. */
function CatalogDetail({ it, onClose, onImport }: { it: CatalogItem; onClose: () => void; onImport: () => void }) {
  const s = useStore();
  const shop = (where: 'lcsc' | 'jlcpcb') =>
    openShop(it.lcsc, where, !!s.app?.canOpenFile).catch((e) => s.toast({ kind: 'err', title: `Could not open ${where === 'lcsc' ? 'LCSC' : 'JLCPCB'}`, body: (e as Error).message }));
  const attrs = Object.entries(it.attributes ?? {}).sort(([a], [b]) => a.localeCompare(b));
  return (
    <aside className="catalog-detail">
      <div className="row">
        <div className="grow">
          <div className="mono" style={{ fontWeight: 700, fontSize: 15 }}>
            {it.mpn}
          </div>
          <div className="faint">
            {it.manufacturer || 'unknown maker'} · {it.lcsc}
          </div>
        </div>
        <button className="btn ghost icon sm" onClick={onClose} aria-label="Close details">
          <X size={14} />
        </button>
      </div>
      <div className="row" style={{ flexWrap: 'wrap', gap: 6 }}>
        <span className={`badge ${libraryTone[it.library]}`}>JLC {it.library}</span>
        <span className="badge">{it.stock.toLocaleString()} in stock</span>
        <span className="badge">{priceText(it.price)} each</span>
        {it.package && <span className="badge">{it.package}</span>}
      </div>
      <div style={{ fontSize: 12 }}>{it.description}</div>
      <div className="faint" style={{ fontSize: 11 }}>
        {[it.category, it.subcategory].filter(Boolean).join(' › ')}
      </div>
      {it.inLibrary ? (
        <button className="btn" onClick={() => s.open({ kind: 'part', title: it.inLibrary!, params: { part: it.inLibrary } })}>
          Open {it.inLibrary} in the library
        </button>
      ) : (
        <button className="btn primary" onClick={onImport}>
          <FileDown size={13} /> Import datasheet
        </button>
      )}
      <div className="row" style={{ gap: 6 }}>
        <button className="btn grow" title={`Buy ${it.mpn} at LCSC (${it.lcsc})`} onClick={() => shop('lcsc')}>
          <ExternalLink size={13} /> LCSC
        </button>
        <button className="btn grow" title={`${it.mpn} on JLCPCB: assembly library, stock and pricing`} onClick={() => shop('jlcpcb')}>
          <ExternalLink size={13} /> JLCPCB
        </button>
      </div>
      {!it.datasheet && <div className="faint" style={{ fontSize: 11 }}>No datasheet link in the catalogue; the part number is looked up at the manufacturer's site.</div>}
      {attrs.length > 0 && (
        <table className="dtable" style={{ fontSize: 12 }}>
          <tbody>
            {attrs.map(([k, v]) => (
              <tr key={k}>
                <td className="faint">{k}</td>
                <td className="mono" style={{ whiteSpace: 'normal', minWidth: 90 }}>
                  {v}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </aside>
  );
}
