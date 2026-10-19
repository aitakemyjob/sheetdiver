import { useRef, useState, type ReactNode } from 'react';
import { Database, FileSpreadsheet, FileUp, FolderOpen, Hash, Link2 } from 'lucide-react';
import { api } from '../lib/api';
import { categories } from '../lib/format';
import { useStore } from '../lib/store';
import type { BOMResult } from '../lib/types';
import { Modal, Switch } from './ui';
import { CatalogSearch, libraryTone } from './CatalogPanel';

type Mode = 'files' | 'catalog' | 'folder' | 'mpns' | 'urls' | 'bom';

/** Splits pasted text into entries (one per line, or separated by commas/spaces). */
export function splitList(text: string): string[] {
  return [...new Set(text.split(/[\s,;]+/).map((t) => t.trim()).filter(Boolean))];
}

/**
 * Import datasheets: PDFs (drop or pick, many at once), a folder, a list of
 * part numbers or URLs, or a BOM that becomes a project.
 */
export function ImportDialog() {
  const s = useStore();
  const [mode, setMode] = useState<Mode>('files');
  const modes: [Mode, string, ReactNode][] = [
    ['files', 'Files', <FileUp size={13} key="f" />],
    ['catalog', 'Catalogue', <Database size={13} key="c" />],
    ['folder', 'Folder', <FolderOpen size={13} key="d" />],
    ['mpns', 'Part numbers', <Hash size={13} key="m" />],
    ['urls', 'URLs', <Link2 size={13} key="u" />],
    ['bom', 'BOM', <FileSpreadsheet size={13} key="b" />],
  ];
  return (
    <Modal title="Import datasheets" icon={<FileUp size={16} style={{ color: 'var(--accent)' }} />} onClose={() => s.setImportOpen(false)} wide={mode === 'bom' || mode === 'catalog'}>
      <div className="seg" role="tablist">
        {modes.map(([m, label, icon]) => (
          <button key={m} role="tab" aria-selected={mode === m} className={mode === m ? 'on' : ''} onClick={() => setMode(m)}>
            {icon} {label}
          </button>
        ))}
      </div>
      {mode === 'files' && <FilesMode />}
      {mode === 'catalog' && <CatalogSearch />}
      {mode === 'folder' && <FolderMode />}
      {mode === 'mpns' && <ListMode kind="mpns" />}
      {mode === 'urls' && <ListMode kind="urls" />}
      {mode === 'bom' && <BOMMode />}
    </Modal>
  );
}

/** Drop or pick PDFs, with optional overrides. */
function FilesMode() {
  const s = useStore();
  const input = useRef<HTMLInputElement>(null);
  const [over, setOver] = useState(false);
  const [mpns, setMpns] = useState('');
  const [mfr, setMfr] = useState('');
  const [cat, setCat] = useState('');
  const opts = { mpns: mpns.trim() || undefined, manufacturer: mfr.trim() || undefined, category: cat || undefined };
  const files = (fs: File[]) => {
    s.setImportOpen(false);
    s.importFiles(fs, opts);
  };
  const pick = async () => {
    if (s.app?.canPickFile) {
      const r = await api.pickPDFs();
      if (r.paths?.length) {
        s.setImportOpen(false);
        s.importPaths(r.paths, { ...opts, mpns: opts.mpns ? splitList(opts.mpns) : undefined });
      }
    } else {
      input.current?.click();
    }
  };
  return (
    <>
      <div
        className={`dropzone ${over ? 'over' : ''}`}
        onClick={pick}
        onDragOver={(e) => {
          e.preventDefault();
          setOver(true);
        }}
        onDragLeave={() => setOver(false)}
        onDrop={(e) => {
          e.preventDefault();
          setOver(false);
          const fs = Array.from(e.dataTransfer.files);
          if (fs.length) files(fs);
        }}
      >
        <FileUp size={26} />
        <div style={{ marginTop: 8, fontWeight: 600 }}>Drop PDF datasheets here, or click to choose</div>
        <div className="faint" style={{ marginTop: 4 }}>Pick as many as you like; they're imported several at a time.</div>
      </div>
      <input
        ref={input}
        type="file"
        accept="application/pdf,.pdf"
        multiple
        hidden
        onChange={(e) => {
          const fs = Array.from(e.target.files ?? []);
          if (fs.length) files(fs);
          e.target.value = '';
        }}
      />
      <details>
        <summary className="muted" style={{ cursor: 'pointer' }}>Overrides (optional, applied to every file)</summary>
        <div className="col" style={{ marginTop: 10 }}>
          <div className="field">
            <label>Part numbers (comma-separated; replaces detection)</label>
            <input className="input" placeholder="e.g. TPS563200, TPS562200" value={mpns} onChange={(e) => setMpns(e.target.value)} />
          </div>
          <div className="row">
            <div className="field grow">
              <label>Manufacturer</label>
              <input className="input" value={mfr} onChange={(e) => setMfr(e.target.value)} />
            </div>
            <div className="field grow">
              <label>Category</label>
              <select className="input select" value={cat} onChange={(e) => setCat(e.target.value)}>
                <option value="">Detect</option>
                {Object.entries(categories).map(([k, v]) => (
                  <option key={k} value={k}>
                    {v}
                  </option>
                ))}
              </select>
            </div>
          </div>
        </div>
      </details>
    </>
  );
}

/** Import every PDF in a folder. */
function FolderMode() {
  const s = useStore();
  const [folder, setFolder] = useState('');
  const [recursive, setRecursive] = useState(true);
  const browse = async () => {
    const r = await api.pickFolder().catch((e) => s.toast({ kind: 'err', title: 'Could not open the folder picker', body: (e as Error).message }));
    if (r?.path) setFolder(r.path);
  };
  return (
    <div className="col">
      <div className="faint">Every PDF in the folder is imported, several at a time. Files already in the library are recognised and skipped.</div>
      <div className="row">
        <input className="input grow mono" placeholder={s.app?.platform === 'windows' ? 'C:\\Users\\me\\Datasheets' : '~/Datasheets'} value={folder} onChange={(e) => setFolder(e.target.value)} />
        {s.app?.canPickFile && (
          <button className="btn" onClick={browse}>
            <FolderOpen size={13} /> Browse
          </button>
        )}
      </div>
      <label className="row" style={{ gap: 8 }}>
        <Switch on={recursive} onChange={setRecursive} label="Include subfolders" /> Include subfolders
      </label>
      <div className="row">
        <div className="grow" />
        <button
          className="btn primary"
          disabled={!folder.trim()}
          onClick={() => {
            s.setImportOpen(false);
            s.queueImports({ folder: folder.trim(), recursive });
          }}
        >
          Import folder
        </button>
      </div>
    </div>
  );
}

/** Import a pasted list of part numbers or URLs. */
function ListMode({ kind }: { kind: 'mpns' | 'urls' }) {
  const s = useStore();
  const [text, setText] = useState('');
  const items = splitList(text);
  const mpns = kind === 'mpns';
  return (
    <div className="col">
      <div className="faint">
        {mpns
          ? "One part number per line (or comma-separated). Each is looked up on the likely manufacturer's site (TI, ST, ADI/Maxim, onsemi, Nexperia, Diodes); ordering suffixes are tried off when needed."
          : 'One URL per line: direct PDF links or product pages (the datasheet link on the page is followed).'}
      </div>
      <textarea className="input mono" rows={8} placeholder={mpns ? 'TPS563200\nMAX77801\nSTM32U575' : 'https://www.ti.com/lit/ds/symlink/tps63900.pdf'} value={text} onChange={(e) => setText(e.target.value)} />
      <div className="row">
        <span className="faint grow">{items.length ? `${items.length} to import` : ''}</span>
        <button
          className="btn primary"
          disabled={!items.length}
          onClick={() => {
            s.setImportOpen(false);
            s.queueImports(mpns ? { mpns: items } : { urls: items });
          }}
        >
          Import {items.length > 1 ? items.length : ''}
        </button>
      </div>
    </div>
  );
}

/** Build a project from a CSV/KiCad BOM. */
function BOMMode() {
  const s = useStore();
  const input = useRef<HTMLInputElement>(null);
  const [text, setText] = useState('');
  const [filename, setFilename] = useState('');
  const [project, setProject] = useState('');
  const [fetchMissing, setFetchMissing] = useState(true);
  const [busy, setBusy] = useState(false);
  const [res, setRes] = useState<BOMResult>();
  const load = async (f: File) => {
    setText(await f.text());
    setFilename(f.name);
    if (!project) setProject(f.name.replace(/\.[^.]+$/, ''));
  };
  const run = async () => {
    setBusy(true);
    try {
      setRes(await api.importBOM({ text, filename: filename || 'BOM.csv', project: project.trim() || undefined, fetch: fetchMissing }));
      s.refreshLibrary();
    } catch (e) {
      s.toast({ kind: 'err', title: 'Could not import the BOM', body: (e as Error).message });
    } finally {
      setBusy(false);
    }
  };
  if (res) {
    return (
      <div className="col">
        <div className="row" style={{ flexWrap: 'wrap', gap: 6 }}>
          <b>Project {res.project}</b>
          <span className="badge ok">{res.counts['in library'] ?? 0} in library</span>
          <span className="badge accent">{res.counts['imported'] ?? 0} imported</span>
          {(res.counts['no datasheet'] ?? 0) > 0 && <span className="badge warn">{res.counts['no datasheet']} without datasheet</span>}
          <span className="badge">{res.counts['skipped'] ?? 0} skipped</span>
        </div>
        <div style={{ maxHeight: 340, overflow: 'auto' }}>
          <table className="dtable">
            <thead>
              <tr>
                <th>Ref</th>
                <th>BOM part</th>
                <th>Qty</th>
                <th>Library part</th>
                <th>JLC</th>
                <th>Result</th>
              </tr>
            </thead>
            <tbody>
              {res.rows.map((r) => (
                <tr key={r.line}>
                  <td className="mono ellipsis" style={{ maxWidth: 120 }} title={r.designators.join(', ')}>
                    {r.designators.join(', ')}
                  </td>
                  <td className="mono">{r.mpn || <span className="faint">{r.value}</span>}</td>
                  <td className="num">{r.qty}</td>
                  <td className="mono">{r.part ?? ''}</td>
                  <td title={r.jlc ? `${r.jlc.lcsc}: ${r.jlc.stock.toLocaleString()} in stock` : 'Not in the JLCPCB catalogue (or the catalogue isn\'t downloaded)'}>
                    {r.jlc ? (
                      <>
                        <span className={`badge ${libraryTone[r.jlc.library] ?? ''}`}>{r.jlc.library}</span> <span className="faint">{r.jlc.stock ? r.jlc.stock.toLocaleString() : 'no stock'}</span>
                      </>
                    ) : (
                      <span className="faint">–</span>
                    )}
                  </td>
                  <td title={r.note}>
                    <span className={`badge ${{ 'in library': 'ok', imported: 'accent', 'no datasheet': 'warn', skipped: '' }[r.status]}`}>{r.status}</span>
                    {r.note && <div className="faint ellipsis" style={{ maxWidth: 260, fontSize: 11 }}>{r.note}</div>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="row">
          <button className="btn" onClick={() => setRes(undefined)}>
            Import another
          </button>
          <div className="grow" />
          <button
            className="btn primary"
            onClick={() => {
              s.setImportOpen(false);
              s.open({ kind: 'project', title: res.project, params: { project: res.project } });
            }}
          >
            Open project
          </button>
        </div>
      </div>
    );
  }
  return (
    <div className="col">
      <div className="faint">
        A CSV or TSV BOM from KiCad, Altium, JLCPCB and the like: a reference/designator column plus an MPN, LCSC (C-number) or value column. Each part is found in the library or its datasheet is fetched by
        part number; parts that can't be fetched still join the project, marked “no datasheet”. Lines without a part number (most passives) are skipped.
      </div>
      <div
        className="dropzone"
        style={{ padding: 16 }}
        onClick={() => input.current?.click()}
        onDragOver={(e) => e.preventDefault()}
        onDrop={(e) => {
          e.preventDefault();
          const f = e.dataTransfer.files[0];
          if (f) load(f);
        }}
      >
        <FileSpreadsheet size={22} />
        <div style={{ marginTop: 6 }}>{filename ? <b>{filename}</b> : 'Drop a BOM file here, click to choose, or paste it below'}</div>
      </div>
      <input
        ref={input}
        type="file"
        accept=".csv,.tsv,.txt,text/csv"
        hidden
        onChange={(e) => {
          const f = e.target.files?.[0];
          if (f) load(f);
          e.target.value = '';
        }}
      />
      <textarea className="input mono" rows={5} placeholder={'Reference,Value,MPN,Qty\nU1,,TPS563200DDCR,1'} value={text} onChange={(e) => setText(e.target.value)} />
      <div className="row">
        <div className="field grow">
          <label>Project (created if new)</label>
          <input className="input" placeholder="default: the file name" value={project} onChange={(e) => setProject(e.target.value)} />
        </div>
        <label className="row" style={{ gap: 8, alignSelf: 'flex-end', paddingBottom: 6 }}>
          <Switch on={fetchMissing} onChange={setFetchMissing} label="Fetch missing datasheets" /> Fetch missing datasheets
        </label>
      </div>
      <div className="row">
        <div className="grow" />
        <button className="btn primary" disabled={!text.trim() || busy} onClick={run}>
          {busy ? 'Importing…' : 'Import BOM'}
        </button>
      </div>
    </div>
  );
}
