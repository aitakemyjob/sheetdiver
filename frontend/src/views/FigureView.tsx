import { useEffect, useState } from 'react';
import { FileText, Plus, Trash2 } from 'lucide-react';
import { api, figureImageURL } from '../lib/api';
import { figureKinds, relTime } from '../lib/format';
import { useStore } from '../lib/store';
import type { Curve, Figure } from '../lib/types';
import { ErrorBox, SourceBadge, Spinner, useAsync } from '../components/ui';
import { NotesPanel } from './NotesPanel';

/** One figure: the image and an editor for what it shows. */
export function FigureView({ params }: { params: Record<string, unknown> }) {
  const s = useStore();
  const id = Number(params.id);
  const data = useAsync(() => api.figure(id), [id, s.libraryVersion]);
  const ds = useAsync(() => (data.data ? api.datasheet(data.data.figure.datasheetId) : Promise.resolve(undefined)), [data.data?.figure.datasheetId]);
  const [f, setF] = useState<Figure>();
  useEffect(() => {
    if (data.data) setF(data.data.figure);
  }, [data.data]);
  useEffect(() => {
    if (f) api.uiContext({ view: 'figure', datasheetId: f.datasheetId, page: f.page, figureId: f.id, part: ds.data?.parts[0]?.mpn }).catch(() => {});
  }, [f, ds.data]);
  if (data.loading && !data.data) return <Spinner label="Loading figure…" />;
  if (data.error) return <ErrorBox error={data.error} />;
  if (!f || !data.data) return null;
  const orig = data.data.figure;
  const dirty = JSON.stringify(f) !== JSON.stringify(orig);
  const mpn = ds.data?.parts[0]?.mpn ?? '';
  const save = async () => {
    try {
      const out = await api.updateFigure(f.id, {
        kind: f.kind, title: f.title ?? '', xAxis: f.xAxis ?? '', yAxis: f.yAxis ?? '', conditions: f.conditions ?? '', description: f.description ?? '',
        curves: f.curves ?? [], caption: f.caption, appliesTo: f.appliesTo ?? [],
      });
      setF(out);
      s.refreshLibrary();
      s.toast({ kind: 'ok', title: 'Figure saved' });
    } catch (e) {
      s.toast({ kind: 'err', title: 'Could not save', body: (e as Error).message });
    }
  };
  const set = (patch: Partial<Figure>) => setF({ ...f, ...patch });
  const setCurve = (i: number, patch: Partial<Curve>) => set({ curves: (f.curves ?? []).map((c, j) => (j === i ? { ...c, ...patch } : c)) });
  return (
    <div className="view">
      <div className="toolbar">
        <span className="title">
          Figure {f.number} · {f.caption}
        </span>
        <span className="badge">{figureKinds[f.kind] ?? f.kind}</span>
        <SourceBadge source={f.source} />
        <div className="grow" />
        {ds.data?.parts.map((p) => (
          <button key={p.id} className="chip" onClick={() => s.open({ kind: 'part', title: p.mpn, params: { part: p.mpn, tab: 'figures' } })}>
            {p.mpn}
          </button>
        ))}
        <button className="btn sm" onClick={() => s.open({ kind: 'page', title: `${mpn} p${f.page + 1}`, params: { ds: f.datasheetId, page: f.page, box: f.box, part: mpn, nonce: Date.now() } })}>
          <FileText size={13} /> Page {f.page + 1}
        </button>
      </div>
      <div className="scroll" style={{ flex: 1 }}>
        <div className="fig-detail">
          <div className="col">
            <div className="fig-image">
              <img src={figureImageURL(f.id, 170, orig.updatedAt)} alt={f.caption} />
            </div>
            <div className="faint" style={{ fontSize: 11.5 }}>{f.section}</div>
            {data.data.text && (
              <details className="card">
                <summary className="muted" style={{ cursor: 'pointer' }}>Text inside the figure</summary>
                <pre className="sec-text" style={{ marginTop: 8 }}>{data.data.text}</pre>
              </details>
            )}
          </div>
          <div className="col">
            <div className="card col">
              <h3>
                What it shows <span className="faint" style={{ textTransform: 'none', letterSpacing: 0, fontWeight: 500 }}>searchable; the agent fills this in after looking</span>
              </h3>
              <div className="row">
                <div className="field grow">
                  <label>Kind</label>
                  <select className="input select" value={f.kind} onChange={(e) => set({ kind: e.target.value })}>
                    {Object.entries(figureKinds).map(([k, v]) => (
                      <option key={k} value={k}>
                        {v}
                      </option>
                    ))}
                  </select>
                </div>
              </div>
              <div className="field">
                <label>Title</label>
                <input className="input" value={f.title ?? ''} placeholder="e.g. Efficiency vs output current, VIN = 12 V" onChange={(e) => set({ title: e.target.value })} />
              </div>
              <div className="row">
                <div className="field grow">
                  <label>Y axis</label>
                  <input className="input" value={f.yAxis ?? ''} onChange={(e) => set({ yAxis: e.target.value })} />
                </div>
                <div className="field grow">
                  <label>X axis</label>
                  <input className="input" value={f.xAxis ?? ''} onChange={(e) => set({ xAxis: e.target.value })} />
                </div>
              </div>
              <div className="field">
                <label>Conditions</label>
                <input className="input" value={f.conditions ?? ''} placeholder="e.g. VOUT = 1.05 V, L = 1.5 µH, TA = 25 °C" onChange={(e) => set({ conditions: e.target.value })} />
              </div>
              <div className="field">
                <label>Description</label>
                <textarea className="input" value={f.description ?? ''} placeholder="Key takeaways, with values read off the graph" onChange={(e) => set({ description: e.target.value })} />
              </div>
              <div className="field">
                <label>
                  Curves
                  <button className="btn ghost sm" style={{ marginLeft: 'auto' }} onClick={() => set({ curves: [...(f.curves ?? []), { label: '' }] })}>
                    <Plus size={12} /> Add
                  </button>
                </label>
                {(f.curves ?? []).map((c, i) => (
                  <div key={i} className="row" style={{ marginBottom: 4 }}>
                    <input className="input" style={{ width: 120 }} placeholder="Label" value={c.label} onChange={(e) => setCurve(i, { label: e.target.value })} />
                    <input className="input grow" placeholder="Conditions / what it shows" value={c.summary ?? c.conditions ?? ''} onChange={(e) => setCurve(i, { summary: e.target.value })} />
                    <button className="btn ghost icon sm" onClick={() => set({ curves: (f.curves ?? []).filter((_, j) => j !== i) })}>
                      <Trash2 size={12} />
                    </button>
                  </div>
                ))}
              </div>
              <div className="row">
                <button className="btn primary" disabled={!dirty} onClick={save}>
                  Save
                </button>
                {dirty && (
                  <button className="btn ghost" onClick={() => setF(orig)}>
                    Revert
                  </button>
                )}
                <span className="faint" style={{ marginLeft: 'auto', fontSize: 11 }}>
                  updated {relTime(orig.updatedAt)}
                </span>
              </div>
            </div>
            <div className="card">
              <h3>Notes</h3>
              <NotesPanel kind="figure" id={String(f.id)} datasheetId={f.datasheetId} />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
