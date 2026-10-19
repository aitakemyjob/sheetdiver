import { useEffect, useMemo, useRef, useState } from 'react';
import { ChevronLeft, ChevronRight, ExternalLink, ImagePlus, Minus, Plus, ScanText } from 'lucide-react';
import { api, openPDF, pageImageURL } from '../lib/api';
import { figureKinds } from '../lib/format';
import { useStore } from '../lib/store';
import type { Rect } from '../lib/types';
import { ErrorBox, Modal, Spinner, useAsync } from '../components/ui';

/**
 * A datasheet's pages: thumbnails, the page at a zoom level with figure
 * and table boxes overlaid, a highlighted region (from a parameter or the
 * agent), and drag-to-select to read a region's text or define a figure.
 */
export function PageView({ params }: { params: Record<string, unknown> }) {
  const s = useStore();
  const ds = Number(params.ds);
  const [page, setPage] = useState(Number(params.page ?? 0));
  const [zoom, setZoom] = useState(1.25);
  const [sel, setSel] = useState<Rect>();
  const [dragFrom, setDragFrom] = useState<{ x: number; y: number }>();
  const [regionText, setRegionText] = useState<string>();
  const [newFig, setNewFig] = useState<Rect>();
  const hl = params.box as Rect | undefined;
  useEffect(() => {
    setPage(Number(params.page ?? 0));
  }, [params.page, params.nonce]);
  const info = useAsync(() => api.datasheet(ds), [ds, s.libraryVersion]);
  const figs = useAsync(() => api.figures(ds, {}), [ds, s.libraryVersion]);
  const tables = useAsync(() => api.tables(ds), [ds, s.libraryVersion]);
  const pages = info.data?.pages ?? [];
  const size = pages[page];
  const mpn = String(params.part ?? info.data?.parts[0]?.mpn ?? '');
  const scale = zoom * 1.4; // CSS px per PDF point
  const main = useRef<HTMLDivElement>(null);
  useEffect(() => {
    api.uiContext({ view: 'page', part: mpn, datasheetId: ds, page }).catch(() => {});
  }, [ds, page, mpn]);
  useEffect(() => {
    // Scroll the highlight into view when the agent points at something.
    if (hl && main.current && Number(params.page) === page) {
      main.current.scrollTo({ top: Math.max(0, hl.y0 * scale - 120), behavior: 'smooth' });
    }
  }, [hl, page, params.page, scale, params.nonce]);
  const onPage = useMemo(() => (figs.data ?? []).filter((f) => f.page === page), [figs.data, page]);
  const tablesOnPage = useMemo(() => (tables.data ?? []).filter((t) => t.page === page), [tables.data, page]);
  if (info.loading && !info.data) return <Spinner label="Loading datasheet…" />;
  if (info.error) return <ErrorBox error={info.error} />;
  if (!size) return null;
  const dpi = Math.round(72 * scale * (window.devicePixelRatio || 1));
  const toPt = (e: React.MouseEvent) => {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    return { x: (e.clientX - r.left) / scale, y: (e.clientY - r.top) / scale };
  };
  const box = (r: Rect) => ({ left: r.x0 * scale, top: r.y0 * scale, width: (r.x1 - r.x0) * scale, height: (r.y1 - r.y0) * scale });
  return (
    <div className="view">
      <div className="toolbar">
        <span className="title">{info.data?.datasheet.filename}</span>
        <button className="btn icon sm" disabled={page <= 0} onClick={() => setPage(page - 1)}>
          <ChevronLeft size={14} />
        </button>
        <span className="mono">
          {page + 1} / {pages.length}
        </span>
        <button className="btn icon sm" disabled={page >= pages.length - 1} onClick={() => setPage(page + 1)}>
          <ChevronRight size={14} />
        </button>
        <span className="sep" />
        <button className="btn icon sm" onClick={() => setZoom(Math.max(0.6, zoom - 0.2))}>
          <Minus size={14} />
        </button>
        <span className="mono">{Math.round(zoom * 100)}%</span>
        <button className="btn icon sm" onClick={() => setZoom(Math.min(3, zoom + 0.2))}>
          <Plus size={14} />
        </button>
        {typeof params.message === 'string' && params.message && <span className="badge violet">{params.message}</span>}
        <div className="grow" />
        <button className="btn sm" onClick={() => openPDF(ds, !!s.app?.canOpenFile).catch((e) => s.toast({ kind: 'err', title: 'Could not open the PDF', body: (e as Error).message }))}>
          <ExternalLink size={13} /> PDF
        </button>
        <span className="faint">Drag on the page to select a region</span>
        {sel && (
          <>
            <button
              className="btn sm"
              onClick={async () => {
                const p = await api.page(ds, page);
                setRegionText((p.lines ?? []).filter((l) => l.b.x0 < sel.x1 && sel.x0 < l.b.x1 && l.b.y0 < sel.y1 && sel.y0 < l.b.y1).map((l) => l.t).join('\n'));
              }}
            >
              <ScanText size={13} /> Text
            </button>
            <button className="btn sm" onClick={() => setNewFig(sel)}>
              <ImagePlus size={13} /> Make figure
            </button>
          </>
        )}
      </div>
      <div className="pv">
        <div className="pv-thumbs">
          {pages.map((p) => (
            <div key={p.page} className={`pv-thumb ${p.page === page ? 'on' : ''}`} onClick={() => setPage(p.page)}>
              <img src={pageImageURL(ds, p.page, 20)} alt="" loading="lazy" style={{ aspectRatio: `${p.width} / ${p.height}` }} />
              <span>{p.page + 1}</span>
            </div>
          ))}
        </div>
        <div className="pv-main" ref={main}>
          <div
            className="pv-page"
            style={{ width: size.width * scale, height: size.height * scale }}
            onMouseDown={(e) => {
              if ((e.target as HTMLElement).closest('.pv-box')) return;
              setDragFrom(toPt(e));
              setSel(undefined);
            }}
            onMouseMove={(e) => {
              if (!dragFrom) return;
              const p = toPt(e);
              setSel({ x0: Math.min(dragFrom.x, p.x), y0: Math.min(dragFrom.y, p.y), x1: Math.max(dragFrom.x, p.x), y1: Math.max(dragFrom.y, p.y) });
            }}
            onMouseUp={() => {
              setDragFrom(undefined);
              if (sel && (sel.x1 - sel.x0 < 6 || sel.y1 - sel.y0 < 6)) setSel(undefined);
            }}
          >
            <img src={pageImageURL(ds, page, Math.min(300, dpi))} alt={`Page ${page + 1}`} draggable={false} />
            {onPage.map((f) => (
              <div key={f.id} className="pv-box figure" style={box(f.box)} title={f.caption} onClick={() => s.open({ kind: 'figure', title: `Fig ${f.number}`, params: { id: f.id } })}>
                <span className="tag">
                  {figureKinds[f.kind] ?? f.kind} {f.number}
                </span>
              </div>
            ))}
            {tablesOnPage.map((t) => (
              <div key={t.id} className="pv-box table" style={box(t.box)} title={`${t.kind} table ${t.caption ?? ''}`} />
            ))}
            {hl && Number(params.page) === page && <div className="pv-box hl" style={box(hl)} />}
            {sel && <div className="pv-select" style={box(sel)} />}
          </div>
        </div>
      </div>
      {regionText !== undefined && (
        <Modal title="Text in region" onClose={() => setRegionText(undefined)}>
          <pre className="snippet" style={{ whiteSpace: 'pre-wrap' }}>{regionText || '(no text)'}</pre>
        </Modal>
      )}
      {newFig && <NewFigure ds={ds} page={page} box={newFig} onClose={() => setNewFig(undefined)} />}
    </div>
  );
}

/** Dialog to define a figure the extractor missed. */
function NewFigure({ ds, page, box, onClose }: { ds: number; page: number; box: Rect; onClose: () => void }) {
  const s = useStore();
  const [caption, setCaption] = useState('');
  const [kind, setKind] = useState('graph');
  return (
    <Modal
      title="New figure"
      onClose={onClose}
      footer={
        <button
          className="btn primary"
          onClick={async () => {
            try {
              const f = await api.addFigure({ datasheetId: ds, page, box, caption, kind });
              s.refreshLibrary();
              s.open({ kind: 'figure', title: 'Figure', params: { id: f.id } });
              onClose();
            } catch (e) {
              s.toast({ kind: 'err', title: 'Could not add figure', body: (e as Error).message });
            }
          }}
        >
          Create
        </button>
      }
    >
      <div className="field">
        <label>Caption</label>
        <input className="input" value={caption} onChange={(e) => setCaption(e.target.value)} autoFocus />
      </div>
      <div className="field">
        <label>Kind</label>
        <select className="input select" value={kind} onChange={(e) => setKind(e.target.value)}>
          {Object.entries(figureKinds).map(([k, v]) => (
            <option key={k} value={k}>
              {v}
            </option>
          ))}
        </select>
      </div>
    </Modal>
  );
}
