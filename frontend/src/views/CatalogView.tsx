import { useEffect, useMemo, useState } from 'react';
import { ChevronDown, ChevronRight, Database, Download, FileDown, RefreshCw, Search, X } from 'lucide-react';
import { api } from '../lib/api';
import { catalogProgressText, useStore } from '../lib/store';
import type { CatalogCategory, CatalogItem } from '../lib/types';
import { libraryTone, priceText } from '../components/CatalogPanel';
import { Empty, ErrorBox, Spinner, Switch, useAsync } from '../components/ui';

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
  const cats = useAsync(() => (c?.installed ? api.catalogCategories() : Promise.resolve([])), [c?.installed, c?.downloaded]);
  const groups = useMemo(() => groupCategories(cats.data ?? [], catFilter, inStock), [cats.data, catFilter, inStock]);

  useEffect(() => {
    api.uiContext({ view: 'catalog', selection: sub || text }).catch(() => {});
  }, [sub, text]);
  useEffect(() => setPage(0), [text, sub, inStock, basic, sort]);
  useEffect(() => {
    if (!c?.installed) return;
    if (!text.trim() && !sub) {
      setItems(undefined);
      return;
    }
    const t = setTimeout(() => {
      setLoading(true);
      api
        .catalogSearch({ text, subcategory: sub || undefined, inStock, basic, sort: sort.key || undefined, desc: sort.desc, limit: pageSize, offset: page * pageSize })
        .then((r) => {
          setItems(r);
          setError(undefined);
        }, setError)
        .finally(() => setLoading(false));
    }, 250);
    return () => clearTimeout(t);
  }, [text, sub, inStock, basic, sort, page, c?.installed, c?.downloaded, s.libraryVersion]);

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
  const sortBy = (key: string) => setSort(sort.key === key ? { key, desc: !sort.desc } : { key, desc: false });
  const arrow = (key: string) => (sort.key === key ? (sort.desc ? ' ↓' : ' ↑') : '');

  return (
    <div className="catalog">
      <aside className="catalog-cats">
        <div className="side-search">
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
                  g.subs.map((x) => (
                    <button key={x.category + x.subcategory} className={`cat-sub ${sub === x.subcategory ? 'on' : ''}`} onClick={() => setSub(sub === x.subcategory ? '' : x.subcategory)} title={`${x.parts.toLocaleString()} parts, ${x.inStock.toLocaleString()} in stock`}>
                      <span className="grow ellipsis">{x.subcategory}</span>
                      <span className="faint">{(inStock ? x.inStock : x.parts).toLocaleString()}</span>
                    </button>
                  ))}
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
            <span className="chip on" onClick={() => setSub('')} title="Clear the category">
              {sub} <X size={11} />
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
            {items && items.length > 0 && (
              <table className="dtable compare">
                <thead>
                  <tr>
                    <th style={{ width: 26 }} />
                    <th className="sortable" onClick={() => sortBy('mpn')}>
                      Part{arrow('mpn')}
                    </th>
                    <th>Description</th>
                    <th>Package</th>
                    <th>JLC</th>
                    <th className="sortable" style={{ textAlign: 'right' }} onClick={() => sortBy('stock')}>
                      Stock{arrow('stock')}
                    </th>
                    <th className="sortable" style={{ textAlign: 'right' }} onClick={() => sortBy('price')}>
                      Price{arrow('price')}
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((it) => (
                    <tr key={it.lcsc} className={detail?.lcsc === it.lcsc ? 'hl' : ''} style={{ cursor: 'pointer' }} onClick={() => setDetail(it)}>
                      <td onClick={(e) => e.stopPropagation()}>
                        <input type="checkbox" checked={picked.has(it.lcsc)} onChange={() => toggle(it.lcsc)} aria-label={`Select ${it.mpn}`} />
                      </td>
                      <td>
                        <div className="mono" style={{ fontWeight: 600 }}>
                          {it.mpn}
                        </div>
                        <div className="faint" style={{ fontSize: 11 }}>
                          {it.lcsc} · {it.manufacturer || 'unknown maker'}
                          {it.inLibrary && (
                            <span className="badge ok" style={{ marginLeft: 6 }}>
                              in library
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="ellipsis" style={{ maxWidth: 360, fontSize: 12 }} title={it.description}>
                        {it.description}
                      </td>
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
      {!it.datasheet && <div className="faint" style={{ fontSize: 11 }}>No datasheet link in the catalogue; the part number is looked up at the manufacturer's site.</div>}
      {attrs.length > 0 && (
        <table className="dtable" style={{ fontSize: 12 }}>
          <tbody>
            {attrs.map(([k, v]) => (
              <tr key={k}>
                <td className="faint">{k}</td>
                <td>{v}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </aside>
  );
}
