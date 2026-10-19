import { useEffect, useState } from 'react';
import { Database, Download, RefreshCw, Search, Trash2 } from 'lucide-react';
import { api, openShop } from '../lib/api';
import { catalogProgressText, sizeText, useStore } from '../lib/store';
import type { CatalogItem } from '../lib/types';
import { ErrorBox, Switch } from './ui';

/** Badge style per JLC library. */
export const libraryTone: Record<string, string> = { basic: 'ok', preferred: 'accent', extended: '' };

/** Formats a unit price in USD. */
export function priceText(p: number): string {
  if (!p) return '–';
  return p >= 1 ? `$${p.toFixed(2)}` : `$${p.toFixed(4).replace(/0+$/, '')}`;
}

/** Download/refresh controls and status of the parts catalogue (Settings card). */
export function CatalogSettings() {
  const s = useStore();
  const c = s.catalog;
  const st = s.app?.config.settings;
  const busy = !!c?.updating;
  return (
    <div className="card col">
      <h3>
        <Database size={14} /> Parts catalogue (JLCPCB / LCSC)
      </h3>
      <div className="muted" style={{ fontSize: 12 }}>
        About 2.5 million parts with stock, prices, JLC assembly library and datasheet links, from the open{' '}
        <a href="https://github.com/yaqwsx/jlcparts" target="_blank" rel="noreferrer">
          jlcparts
        </a>{' '}
        dataset (republished from JLCPCB several times a day). It's downloaded only when you ask (about 200 MB, 1.4 GB on disk) and searched offline; you choose which parts' datasheets to import.
      </div>
      {c?.installed ? (
        <div className="row" style={{ flexWrap: 'wrap', gap: 8 }}>
          <span className={`badge ${c.stale ? 'warn' : 'ok'}`}>{c.stale ? `${c.ageDays} days old` : 'up to date'}</span>
          <span>
            {c.parts.toLocaleString()} parts, {c.inStock.toLocaleString()} in stock · published {new Date(c.published).toLocaleDateString()} · {sizeText(c.sizeBytes)}
          </span>
        </div>
      ) : (
        !busy && <div className="faint">Not downloaded.</div>
      )}
      {busy && (
        <div>
          <div className="faint">{catalogProgressText(c)}</div>
          <div className="bar">
            <i style={{ width: `${c?.progress?.total ? (100 * c.progress.done) / c.progress.total : 5}%` }} />
          </div>
        </div>
      )}
      {c?.error && !busy && <ErrorBox error={new Error(c.error)} />}
      <div className="row" style={{ flexWrap: 'wrap' }}>
        <button className="btn primary" disabled={busy} onClick={() => s.downloadCatalog()}>
          {c?.installed ? <RefreshCw size={13} /> : <Download size={13} />} {c?.installed ? 'Refresh now' : 'Download catalogue'}
        </button>
        {c?.installed && (
          <button
            className="btn ghost"
            disabled={busy}
            onClick={async () => useStore.setState({ catalog: await api.catalogRemove().catch((e) => (s.toast({ kind: 'err', title: 'Could not remove', body: (e as Error).message }), c)) })}
          >
            <Trash2 size={13} /> Remove
          </button>
        )}
        <div className="grow" />
        <label className="row" style={{ gap: 6 }}>
          Remind me to refresh after
          <select className="input select" style={{ width: 110 }} value={st?.catalogRemindDays ?? 0} onChange={(e) => s.saveSettings({ catalogRemindDays: Number(e.target.value) })}>
            <option value={7}>7 days</option>
            <option value={0}>14 days</option>
            <option value={30}>30 days</option>
            <option value={90}>90 days</option>
            <option value={-1}>never</option>
          </select>
        </label>
      </div>
    </div>
  );
}

/** Search the catalogue and import the datasheets of the chosen parts. */
export function CatalogSearch() {
  const s = useStore();
  const c = s.catalog;
  const [text, setText] = useState('');
  const [inStock, setInStock] = useState(true);
  const [basic, setBasic] = useState(false);
  const [items, setItems] = useState<CatalogItem[]>();
  const [error, setError] = useState<unknown>();
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!c?.installed || text.trim().length < 2) {
      setItems(undefined);
      return;
    }
    const t = setTimeout(() => {
      setBusy(true);
      api
        .catalogSearch({ text, inStock, basic, limit: 100 })
        .then((r) => {
          setItems(r);
          setError(undefined);
        }, setError)
        .finally(() => setBusy(false));
    }, 300);
    return () => clearTimeout(t);
  }, [text, inStock, basic, c?.installed, c?.published]);

  if (!c?.installed) {
    return (
      <div className="col">
        <div className="muted">Search JLCPCB's catalogue of about 2.5 million parts (stock, price, assembly library) and import the datasheets of the ones you want. Download it once; it's searched offline.</div>
        {c?.updating ? (
          <div>
            <div className="faint">{catalogProgressText(c)}</div>
            <div className="bar">
              <i style={{ width: `${c.progress?.total ? (100 * c.progress.done) / c.progress.total : 5}%` }} />
            </div>
          </div>
        ) : (
          <div>
            <button className="btn primary" onClick={() => s.downloadCatalog()}>
              <Download size={13} /> Download catalogue (about 200 MB)
            </button>
          </div>
        )}
      </div>
    );
  }
  const toggle = (lcsc: string) => {
    const n = new Set(picked);
    if (n.has(lcsc)) n.delete(lcsc);
    else n.add(lcsc);
    setPicked(n);
  };
  const importPicked = async () => {
    try {
      const jobs = await api.catalogImport([...picked]);
      s.setImportOpen(false);
      s.setQueueOpen(true);
      s.toast({ kind: 'info', title: `Importing ${jobs.length} datasheet${jobs.length === 1 ? '' : 's'}` });
    } catch (e) {
      setError(e);
    }
  };
  return (
    <div className="col">
      <div className="row">
        <Search size={14} className="faint" />
        <input className="input grow" autoFocus placeholder="Part number or words: TPS563200 · buck 3A SOT-23 · 10k 0402 · AO3400" value={text} onChange={(e) => setText(e.target.value)} />
      </div>
      <div className="row" style={{ gap: 16, flexWrap: 'wrap' }}>
        <label className="row" style={{ gap: 6 }}>
          <Switch on={inStock} onChange={setInStock} label="In stock only" /> In stock
        </label>
        <label className="row" style={{ gap: 6 }} title="JLCPCB basic and preferred parts have no extended-part setup fee">
          <Switch on={basic} onChange={setBasic} label="Basic parts only" /> Basic / preferred only
        </label>
        <span className="grow" />
        {c.stale && (
          <button className="btn sm ghost" onClick={() => s.downloadCatalog()} disabled={c.updating} title="Stock and prices change daily">
            <RefreshCw size={12} /> {c.updating ? catalogProgressText(c) : `${c.ageDays} days old · refresh`}
          </button>
        )}
      </div>
      <ErrorBox error={error} />
      <div style={{ maxHeight: 360, overflow: 'auto' }}>
        {busy && !items && <div className="faint">Searching…</div>}
        {items && items.length === 0 && <div className="faint">No parts match.</div>}
        {items && items.length > 0 && (
          <table className="dtable">
            <thead>
              <tr>
                <th style={{ width: 24 }} />
                <th>Part</th>
                <th>Description</th>
                <th>Package</th>
                <th>JLC</th>
                <th style={{ textAlign: 'right' }}>Stock</th>
                <th style={{ textAlign: 'right' }}>Price</th>
              </tr>
            </thead>
            <tbody>
              {items.map((it) => (
                <tr key={it.lcsc} className={picked.has(it.lcsc) ? 'hl' : ''} style={{ cursor: 'pointer' }} onClick={() => toggle(it.lcsc)}>
                  <td>
                    <input type="checkbox" checked={picked.has(it.lcsc)} onChange={() => toggle(it.lcsc)} onClick={(e) => e.stopPropagation()} aria-label={`Select ${it.mpn}`} />
                  </td>
                  <td>
                    <div className="mono" style={{ fontWeight: 600 }}>
                      {it.mpn}
                    </div>
                    <div className="faint" style={{ fontSize: 11 }}>
                      <a
                        className="shop-link"
                        title={`Open ${it.lcsc} at LCSC`}
                        onClick={(e) => {
                          e.stopPropagation();
                          openShop(it.lcsc, 'lcsc', !!s.app?.canOpenFile).catch((err) => s.toast({ kind: 'err', title: 'Could not open LCSC', body: (err as Error).message }));
                        }}
                      >
                        {it.lcsc}
                      </a>{' '}
                      · {it.manufacturer || 'unknown maker'}
                      {it.inLibrary && <span className="badge ok" style={{ marginLeft: 6 }}>in library</span>}
                    </div>
                  </td>
                  <td className="ellipsis" style={{ maxWidth: 280, fontSize: 12 }} title={it.description}>
                    {it.description}
                    {!it.datasheet && <span className="badge warn" style={{ marginLeft: 6 }} title="No datasheet link; the part number will be looked up">no link</span>}
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
      </div>
      <div className="row">
        <span className="faint grow">
          {items ? `${items.length} shown` : 'Type at least two characters.'}
          {picked.size > 0 && ` · ${picked.size} selected`}
        </span>
        <button className="btn primary" disabled={!picked.size} onClick={importPicked}>
          Import {picked.size || ''} datasheet{picked.size === 1 ? '' : 's'}
        </button>
      </div>
    </div>
  );
}
