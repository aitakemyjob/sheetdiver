import { useEffect, useMemo, useRef, useState, type PointerEvent } from 'react';
import { Download, LayoutGrid, Maximize2, Plus, Trash2, X, ZoomIn, ZoomOut } from 'lucide-react';
import { api, saveText } from '../lib/api';
import { layoutBlockDiagram, type Kind, type DrawBox } from '../lib/blockDiagram';
import { useStore } from '../lib/store';
import type { ConnectionRow, ConnectionsChange, Pin, ProjectDetail } from '../lib/types';
import { Empty } from '../components/ui';
import { Checks } from './ProjectView';

/** Applies a change to the connection table, telling the user what couldn't be done. */
async function change(project: string, ch: ConnectionsChange, done: () => void) {
  const s = useStore.getState();
  try {
    const { problems } = await api.changeConnections(project, ch);
    if (problems.length) s.toast({ kind: 'warn', title: 'Not everything was applied', body: problems.join('\n') });
  } catch (e) {
    s.toast({ kind: 'err', title: 'Could not change the connections', body: (e as Error).message });
  }
  done();
}

/** Whether the agent's highlight names this node or link. */
function marked(hl: string | undefined, ...names: (string | undefined)[]): boolean {
  return !!hl && names.some((n) => n?.toLowerCase() === hl);
}

/** Fill, stroke and text colours of a kind of part. */
type Swatch = { fill: string; stroke: string; text: string };

/** Colours by kind, for light and dark themes (as an electronics sketch: ICs violet, R and C green, diodes red, inductors amber). */
const palettes: Record<'light' | 'dark', Record<Kind | 'wire', Swatch>> = {
  light: {
    ic: { fill: '#EEEDFE', stroke: '#534AB7', text: '#3C3489' },
    connector: { fill: '#F1EFE8', stroke: '#5F5E5A', text: '#444441' },
    module: { fill: '#F1EFE8', stroke: '#5F5E5A', text: '#444441' },
    resistor: { fill: '#E1F5EE', stroke: '#0F6E56', text: '#085041' },
    capacitor: { fill: '#E1F5EE', stroke: '#0F6E56', text: '#085041' },
    diode: { fill: '#FAECE7', stroke: '#993C1D', text: '#712B13' },
    inductor: { fill: '#FAEEDA', stroke: '#854F0B', text: '#633806' },
    other: { fill: '#F1EFE8', stroke: '#5F5E5A', text: '#444441' },
    wire: { fill: 'none', stroke: '#898781', text: '#52514E' },
  },
  dark: {
    ic: { fill: '#26215C', stroke: '#AFA9EC', text: '#CECBF6' },
    connector: { fill: '#2C2C2A', stroke: '#B4B2A9', text: '#D3D1C7' },
    module: { fill: '#2C2C2A', stroke: '#B4B2A9', text: '#D3D1C7' },
    resistor: { fill: '#04342C', stroke: '#5DCAA5', text: '#9FE1CB' },
    capacitor: { fill: '#04342C', stroke: '#5DCAA5', text: '#9FE1CB' },
    diode: { fill: '#4A1B0C', stroke: '#F0997B', text: '#F5C4B3' },
    inductor: { fill: '#412402', stroke: '#EF9F27', text: '#FAC775' },
    other: { fill: '#2C2C2A', stroke: '#B4B2A9', text: '#D3D1C7' },
    wire: { fill: 'none', stroke: '#8B8A85', text: '#B4B2A9' },
  },
};

/** Whether the theme is dark, from its background colour. */
function darkTheme(): boolean {
  const bg = getComputedStyle(document.documentElement).getPropertyValue('--bg').trim();
  const m = /^#?([\da-f]{2})([\da-f]{2})([\da-f]{2})/i.exec(bg);
  if (!m) return false;
  const [r, g, b] = m.slice(1).map((h) => parseInt(h, 16));
  return 0.299 * r + 0.587 * g + 0.114 * b < 128;
}

/**
 * The project's block diagram: parts and connectors with their pins,
 * wires labelled with their nets, and passives hanging off what they
 * serve. Parts can be dragged; where they're dropped is saved.
 */
export function BlockDiagram({ d, highlight, onChange }: { d: ProjectDetail; highlight?: string; onChange: () => void }) {
  const s = useStore();
  const [moved, setMoved] = useState<Record<string, { x: number; y: number }>>({});
  const [drag, setDrag] = useState<{ id: string; dx: number; dy: number } | null>(null);
  const svg = useRef<SVGSVGElement>(null);
  const content = useRef<SVGGElement>(null);
  // The view: zoom k and pan x, y (screen pixels); null until fitted.
  const [view, setView] = useState<{ k: number; x: number; y: number } | null>(null);
  const [pan, setPan] = useState<{ x: number; y: number; vx: number; vy: number } | null>(null);
  useEffect(() => setMoved({}), [d]);
  const theme = s.app?.config.settings.theme;
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const pal = useMemo(() => palettes[darkTheme() ? 'dark' : 'light'], [theme]);
  const sch = useMemo(() => {
    const nodes = d.connections.diagram.nodes.map((n) => ({ ...n, ...moved[n.id] }));
    for (const [id, pos] of Object.entries(moved)) if (!nodes.some((n) => n.id === id)) nodes.push({ id, label: id, ...pos });
    return layoutBlockDiagram({ ...d, connections: { ...d.connections, diagram: { ...d.connections.diagram, nodes } } });
  }, [d, moved]);
  const height = sch.height + (sch.grounded ? 24 : 0);
  /** Fits the whole drawing in the view (never above 100%). */
  const fit = () => {
    const el = svg.current;
    if (!el || !el.clientWidth) return setView({ k: 1, x: 0, y: 0 });
    const k = Math.min(1, el.clientWidth / sch.width, el.clientHeight / height);
    setView({ k, x: (el.clientWidth - sch.width * k) / 2, y: Math.max(0, (el.clientHeight - height * k) / 2) });
  };
  /** Zooms by factor about a point of the view (its centre by default). */
  const zoom = (factor: number, cx?: number, cy?: number) =>
    setView((v) => {
      const el = svg.current;
      const cur = v ?? { k: 1, x: 0, y: 0 };
      const k = Math.min(4, Math.max(0.15, cur.k * factor));
      const px = cx ?? (el?.clientWidth ?? 0) / 2;
      const py = cy ?? (el?.clientHeight ?? 0) / 2;
      return { k, x: px - ((px - cur.x) * k) / cur.k, y: py - ((py - cur.y) * k) / cur.k };
    });
  // Fitted when first shown.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => fit(), [sch.boxes.length === 0]);
  // The wheel zooms about the pointer (a native listener: React's is passive).
  useEffect(() => {
    const el = svg.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const r = el.getBoundingClientRect();
      zoom(Math.exp(-e.deltaY * 0.0015), e.clientX - r.left, e.clientY - r.top);
    };
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sch.boxes.length === 0]);
  if (sch.boxes.length === 0) {
    return (
      <Empty icon={<LayoutGrid size={28} />} title="Nothing connected yet">
        Put pins on nets in the Connections tab, or ask your agent to draft the block diagram (set_connections).
      </Empty>
    );
  }
  const at = (e: PointerEvent) => {
    // Screen to drawing coordinates, through the zoom and pan.
    const m = content.current?.getScreenCTM?.();
    return m && typeof DOMPoint !== 'undefined' ? new DOMPoint(e.clientX, e.clientY).matrixTransform(m.inverse()) : { x: e.clientX, y: e.clientY };
  };
  const down = (e: PointerEvent, b: DrawBox) => {
    const p = at(e);
    (e.target as Element).setPointerCapture?.(e.pointerId);
    setDrag({ id: b.id, dx: p.x - b.x, dy: p.y - b.y });
  };
  const startPan = (e: PointerEvent) => {
    if (e.target !== svg.current) return; // the background, not a part
    (e.target as Element).setPointerCapture?.(e.pointerId);
    setPan({ x: e.clientX, y: e.clientY, vx: view?.x ?? 0, vy: view?.y ?? 0 });
  };
  const move = (e: PointerEvent) => {
    if (pan) {
      const p = pan;
      setView((v) => ({ k: v?.k ?? 1, x: p.vx + e.clientX - p.x, y: p.vy + e.clientY - p.y }));
      return;
    }
    if (!drag) return;
    const p = at(e);
    setMoved((m) => ({ ...m, [drag.id]: { x: Math.max(0, Math.round(p.x - drag.dx)), y: Math.max(0, Math.round(p.y - drag.dy)) } }));
  };
  const up = () => {
    setPan(null);
    if (!drag) return;
    const pos = moved[drag.id];
    setDrag(null);
    if (pos) change(d.project.name, { positions: [{ node: drag.id, ...pos }] }, onChange);
  };
  const open = (b: DrawBox) => {
    const pp = d.parts.find((p) => `part:${p.id}` === b.id);
    if (pp) s.open({ kind: 'part', title: pp.mpn, params: { part: pp.mpn } });
  };
  const exportSVG = async () => {
    if (!svg.current) return;
    const css = getComputedStyle(document.documentElement);
    // The whole drawing at full size, whatever the view.
    const copy = svg.current.cloneNode(true) as SVGSVGElement;
    copy.setAttribute('width', String(sch.width));
    copy.setAttribute('height', String(height));
    copy.setAttribute('viewBox', `0 0 ${sch.width} ${height}`);
    copy.removeAttribute('style');
    copy.querySelector('g[data-view]')?.removeAttribute('transform');
    const text = copy.outerHTML.replace(/var\((--[\w-]+)\)/g, (_, v: string) => css.getPropertyValue(v).trim() || 'sans-serif');
    const where = await saveText(`${d.project.name}-block-diagram.svg`, `<?xml version="1.0" encoding="UTF-8"?>\n${text}`, 'image/svg+xml', !!s.app?.canSaveFile);
    if (where) s.toast({ kind: 'ok', title: 'Block diagram saved', body: where });
  };
  const hl = highlight?.toLowerCase();
  const w = pal.wire;
  const font = { fontFamily: 'var(--font-ui)' };
  return (
    <div className="col" style={{ gap: 8 }}>
      <div className="row">
        <span className="faint grow">Drawn from the connection table. Scroll to zoom, drag the background to pan, drag a part to move it; double-click a part to open it.</span>
        <div className="seg" role="group" aria-label="Zoom">
          <button title="Zoom out" aria-label="Zoom out" onClick={() => zoom(1 / 1.25)}>
            <ZoomOut size={13} />
          </button>
          <button title="Fit the drawing" aria-label="Fit" onClick={fit} style={{ minWidth: 52 }}>
            {Math.round((view?.k ?? 1) * 100)}%
          </button>
          <button title="Zoom in" aria-label="Zoom in" onClick={() => zoom(1.25)}>
            <ZoomIn size={13} />
          </button>
          <button title="Fit the drawing" aria-label="Fit the drawing" onClick={fit}>
            <Maximize2 size={13} />
          </button>
        </div>
        <button className="btn sm" onClick={() => change(d.project.name, { resetLayout: true }, onChange)} title="Forget moved parts">
          <LayoutGrid size={13} /> Auto layout
        </button>
        <button className="btn sm" onClick={exportSVG}>
          <Download size={13} /> SVG
        </button>
      </div>
      <div className="card" style={{ overflow: 'hidden', padding: 0 }}>
        <svg
          ref={svg}
          xmlns="http://www.w3.org/2000/svg"
          width="100%"
          role="img"
          aria-label={`Block diagram of ${d.project.name}`}
          onPointerDown={startPan}
          onPointerMove={move}
          onPointerUp={up}
          style={{ display: 'block', userSelect: 'none', height: 'max(380px, calc(100vh - 340px))', cursor: pan ? 'grabbing' : 'default', touchAction: 'none', ...font }}
        >
          <defs>
            <marker id="sd-arrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
              <path d="M2 1L8 5L2 9" fill="none" stroke={w.stroke} strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
            </marker>
          </defs>
          <g ref={content} data-view transform={`translate(${view?.x ?? 0},${view?.y ?? 0}) scale(${view?.k ?? 1})`}>
            {sch.wires.map((x, i) => {
              const on = marked(hl, x.net);
              return (
                <g key={i} data-net={x.net} data-hl={on || undefined}>
                  <path d={x.path} fill="none" stroke={on ? 'var(--accent)' : w.stroke} strokeWidth={on ? 2.5 : x.thin ? 1 : 1.5} markerEnd={x.arrow ? 'url(#sd-arrow)' : undefined} />
                  {x.label && (
                    <text x={x.label.x} y={x.label.y} textAnchor={x.label.anchor} fontSize={12} fill={on ? 'var(--accent)' : w.text} fontWeight={on ? 700 : 400}>
                      {x.label.text}
                    </text>
                  )}
                </g>
              );
            })}
            {sch.boxes.map((b) => {
              const c = pal[b.kind];
              const on = marked(hl, b.title, b.title.split(' ')[0], d.parts.find((p) => `part:${p.id}` === b.id)?.mpn);
              const bottom = b.pins.filter((p) => p.side === 'b');
              return (
                <g key={b.id} data-part={b.title} data-hl={on || undefined} onPointerDown={(e) => down(e, b)} onDoubleClick={() => open(b)} style={{ cursor: drag?.id === b.id ? 'grabbing' : 'grab' }}>
                  <text x={b.x + b.w / 2} y={b.y - 12} textAnchor="middle" fontSize={14} fontWeight={600} fill={c.text}>
                    {b.title}
                  </text>
                  <rect x={b.x} y={b.y} width={b.w} height={b.h} rx={4} fill={c.fill} stroke={on ? 'var(--accent)' : c.stroke} strokeWidth={on ? 2.5 : 0.8} />
                  {b.pins.map((p, i) =>
                    p.side === 'b' ? (
                      <text key={i} x={p.x} y={b.y + b.h - 12} textAnchor="middle" fontSize={12} fill={c.text}>
                        {p.name}
                      </text>
                    ) : (
                      <text key={i} x={p.side === 'r' ? p.x - 12 : p.x + 12} y={p.y} dominantBaseline="central" textAnchor={p.side === 'r' ? 'end' : 'start'} fontSize={12} fill={c.text}>
                        {p.tied ? `${p.name}→${p.tied}` : p.name}
                      </text>
                    ),
                  )}
                  {b.ground.length > 0 && (
                    <text x={b.x + b.w - 12} y={bottom.length ? b.y + b.h - 12 : b.y + b.h - 12} textAnchor="end" fontSize={12} fill={c.text}>
                      {b.ground.join(', ')}
                    </text>
                  )}
                </g>
              );
            })}
            {sch.passives.map((p) => {
              const c = pal[p.kind];
              const on = marked(hl, ...p.label.split(','));
              return (
                <g key={p.key} data-passive={p.label} data-hl={on || undefined}>
                  <rect x={p.x} y={p.y} width={p.w} height={p.h} rx={4} fill={c.fill} stroke={on ? 'var(--accent)' : c.stroke} strokeWidth={on ? 2.5 : 0.8} />
                  <text x={p.x + p.w / 2} y={p.y + (p.value && !p.oneLine ? 17 : p.h / 2)} dominantBaseline="central" textAnchor="middle" fontSize={p.oneLine ? 12 : 13} fontWeight={500} fill={c.text}>
                    {p.label}
                    {p.oneLine && p.value ? <tspan fontWeight={400}> {p.value}</tspan> : null}
                  </text>
                  {p.value && !p.oneLine && (
                    <text x={p.x + p.w / 2} y={p.y + 32} dominantBaseline="central" textAnchor="middle" fontSize={11} fill={c.text}>
                      {p.value}
                    </text>
                  )}
                </g>
              );
            })}
            {sch.grounded && (
              <text x={40} y={sch.height + 8} fontSize={12} fill={w.text}>
                Passives on stubs return to GND.
              </text>
            )}
          </g>
        </svg>
      </div>
    </div>
  );
}

/** The CSV of a connection table. */
export function connectionsCSV(d: ProjectDetail): string {
  const q = (v: unknown) => {
    const t = String(v ?? '');
    return /[",\n]/.test(t) ? `"${t.replace(/"/g, '""')}"` : t;
  };
  const nets = new Map(d.connections.nets.map((n) => [n.id, n]));
  const lines = [['Net', 'Kind', 'Bus', 'Part', 'Pin', 'Pin name', 'Pin type', 'Notes'].join(',')];
  for (const r of d.connections.connections) {
    const n = nets.get(r.netId);
    lines.push([r.net, n?.kind, n?.bus, r.endpoint, r.pin, r.pinName, r.pinType, r.notes].map(q).join(','));
  }
  return lines.join('\n') + '\n';
}

/**
 * The connection table: each net with the pins on it, editable, with the
 * checks against the parts' pin tables on top.
 */
export function ConnectionTable({ d, highlight, onChange }: { d: ProjectDetail; highlight?: string; onChange: () => void }) {
  const s = useStore();
  const c = d.connections;
  const project = d.project.name;
  const checks = (d.checks ?? []).filter((x) => x.area === 'connections');
  const [net, setNet] = useState('');
  const [part, setPart] = useState('');
  const [pin, setPin] = useState('');
  const [block, setBlock] = useState('');
  const [blockKind, setBlockKind] = useState('');
  const [blockValue, setBlockValue] = useState('');
  const [pins, setPins] = useState<Pin[]>([]);
  const entry = d.parts.find((p) => (p.designator || p.mpn) === part);
  useEffect(() => {
    setPins([]);
    if (entry?.datasheetId) api.pins(entry.datasheetId).then(setPins, () => {});
  }, [entry?.datasheetId]);
  const hl = highlight?.toLowerCase();
  const add = () => {
    if (!net.trim() || !part) return;
    change(project, { connect: [{ net: net.trim(), part, pin: pin.trim() || undefined }] }, onChange);
    setPin('');
  };
  const remove = (r: ConnectionRow) => change(project, { connect: [{ net: r.net, part: r.endpoint, pin: r.pin, remove: true }] }, onChange);
  const exportCSV = async () => {
    const where = await saveText(`${project}-connections.csv`, connectionsCSV(d), 'text/csv', !!s.app?.canSaveFile);
    if (where) s.toast({ kind: 'ok', title: 'Connections saved', body: where });
  };
  const endpoints = [...d.parts.map((p) => p.designator || p.mpn), ...c.blocks.map((b) => b.name)];
  return (
    <div className="col" style={{ gap: 16 }}>
      {checks.length > 0 && (
        <div className="card">
          <h3>Checks against the pin tables</h3>
          <Checks checks={checks} />
        </div>
      )}
      <div className="card col" style={{ gap: 8 }}>
        <div className="row">
          <h3 className="grow" style={{ margin: 0 }}>
            Connections
          </h3>
          <button className="btn sm" onClick={exportCSV} disabled={c.connections.length === 0}>
            <Download size={13} /> CSV
          </button>
        </div>
        <form
          className="row"
          style={{ flexWrap: 'wrap' }}
          onSubmit={(e) => {
            e.preventDefault();
            add();
          }}
        >
          <input className="input" style={{ width: 140 }} placeholder="Net (SDA, 3V3…)" aria-label="Net" list="sd-nets" value={net} onChange={(e) => setNet(e.target.value)} />
          <datalist id="sd-nets">
            {c.nets.map((n) => (
              <option key={n.id} value={n.name} />
            ))}
          </datalist>
          <select className="input" style={{ width: 150 }} aria-label="Part or block" value={part} onChange={(e) => setPart(e.target.value)}>
            <option value="">Part or block…</option>
            {endpoints.map((e) => (
              <option key={e} value={e}>
                {e}
              </option>
            ))}
          </select>
          <input className="input" style={{ width: 150 }} placeholder={entry ? 'Pin (number or name)' : 'Pin'} aria-label="Pin" list="sd-pins" value={pin} onChange={(e) => setPin(e.target.value)} />
          <datalist id="sd-pins">
            {pins.map((p) => (
              <option key={p.id} value={p.number}>
                {p.name}
                {p.package ? ` (${p.package})` : ''}
              </option>
            ))}
          </datalist>
          <button className="btn sm primary" type="submit" disabled={!net.trim() || !part}>
            <Plus size={13} /> Connect
          </button>
        </form>
        {c.nets.length === 0 ? (
          <div className="muted">No connections yet. Put pins on nets above, or ask your agent to draft them (set_connections).</div>
        ) : (
          <table className="dtable">
            <thead>
              <tr>
                <th>Net</th>
                <th>Part</th>
                <th>Pin</th>
                <th>Name</th>
                <th>Type</th>
                <th>Notes</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {c.nets.map((n) => {
                const rows = c.connections.filter((r) => r.netId === n.id);
                const on = marked(hl, n.name, n.bus);
                return [
                  <tr key={`n${n.id}`} className={on ? 'agent-hl' : undefined}>
                    <td colSpan={7}>
                      <div className="row">
                        <b className="mono">{n.name}</b>
                        <select
                          className="input"
                          aria-label={`${n.name} kind`}
                          value={n.kind}
                          onChange={(e) => change(project, { nets: [{ name: n.name, kind: e.target.value }] }, onChange)}
                          style={{ width: 90, height: 26 }}
                        >
                          <option value="signal">signal</option>
                          <option value="power">power</option>
                          <option value="ground">ground</option>
                        </select>
                        <input
                          className="input"
                          style={{ width: 110, height: 26 }}
                          placeholder="Bus (I2C1)"
                          aria-label={`${n.name} bus`}
                          defaultValue={n.bus ?? ''}
                          onBlur={(e) => e.target.value !== (n.bus ?? '') && change(project, { nets: [{ name: n.name, bus: e.target.value }] }, onChange)}
                        />
                        <span className="grow" />
                        <button className="btn ghost icon sm" title={`Delete ${n.name} and its connections`} onClick={() => change(project, { nets: [{ name: n.name, remove: true }] }, onChange)}>
                          <Trash2 size={12} />
                        </button>
                      </div>
                    </td>
                  </tr>,
                  ...rows.map((r) => (
                    <tr key={r.id} className={marked(hl, r.endpoint) ? 'agent-hl' : undefined}>
                      <td />
                      <td>{r.endpoint}</td>
                      <td className="mono">{r.pin}</td>
                      <td className="mono">{r.pinName}</td>
                      <td className="faint">{r.pinType}</td>
                      <td className="muted">{r.notes}</td>
                      <td>
                        <button className="btn ghost icon sm" title="Disconnect" onClick={() => remove(r)}>
                          <X size={12} />
                        </button>
                      </td>
                    </tr>
                  )),
                ];
              })}
            </tbody>
          </table>
        )}
      </div>
      <div className="card col" style={{ gap: 8 }}>
        <h3 style={{ margin: 0 }}>Blocks off the BOM</h3>
        <div className="muted">A connector, a battery, a module still to be chosen, or a passive with its value (R4 36.5k, C1 10µF): what the block diagram shows that isn't a part in the BOM.</div>
        <div className="row" style={{ flexWrap: 'wrap' }}>
          {c.blocks.map((b) => (
            <span key={b.id} className="chip">
              {b.name}
              {b.value ? <span className="faint">{b.value}</span> : null}
              <button className="btn ghost icon sm" title={`Remove ${b.name}`} onClick={() => change(project, { blocks: [{ name: b.name, remove: true }] }, onChange)}>
                <X size={11} />
              </button>
            </span>
          ))}
          <form
            className="row"
            onSubmit={(e) => {
              e.preventDefault();
              if (block.trim()) change(project, { blocks: [{ name: block.trim(), kind: blockKind || undefined, value: blockValue.trim() || undefined }] }, onChange);
              setBlock('');
              setBlockValue('');
            }}
          >
            <input className="input" style={{ width: 140 }} placeholder="J1 USB-C, R4…" aria-label="New block" value={block} onChange={(e) => setBlock(e.target.value)} />
            <select className="input" style={{ width: 120 }} aria-label="Block kind" value={blockKind} onChange={(e) => setBlockKind(e.target.value)}>
              <option value="">block</option>
              {['connector', 'module', 'resistor', 'capacitor', 'inductor', 'diode'].map((k) => (
                <option key={k} value={k}>
                  {k}
                </option>
              ))}
            </select>
            <input className="input" style={{ width: 90 }} placeholder="Value" aria-label="Block value" value={blockValue} onChange={(e) => setBlockValue(e.target.value)} />
            <button className="btn sm" type="submit" disabled={!block.trim()}>
              <Plus size={13} /> Block
            </button>
          </form>
        </div>
      </div>
    </div>
  );
}

/** The CSV of the pin tables: one row per pin of each part. */
export function pinoutsCSV(d: ProjectDetail): string {
  const q = (v: unknown) => {
    const t = String(v ?? '');
    return /[",\n]/.test(t) ? `"${t.replace(/"/g, '""')}"` : t;
  };
  const lines = [['Part', 'Pin', 'Name', 'Type', 'Net', 'Connected to', 'Description'].join(',')];
  for (const po of d.connections.pinouts ?? []) for (const p of po.pins) lines.push([po.title, p.number, p.name, p.type, p.net, p.to.join('; '), p.description].map(q).join(','));
  return lines.join('\n') + '\n';
}

/**
 * A reference table per part: every pin (from its datasheet's pin table,
 * connected or not) with its net and what else is on it.
 */
export function PinTables({ d, highlight }: { d: ProjectDetail; highlight?: string }) {
  const s = useStore();
  const pinouts = d.connections.pinouts ?? [];
  const hl = highlight?.toLowerCase();
  if (pinouts.length === 0) {
    return (
      <Empty icon={<LayoutGrid size={28} />} title="No pins yet">
        Parts with a pin table in their datasheet show here, with what each pin is connected to.
      </Empty>
    );
  }
  const exportCSV = async () => {
    const where = await saveText(`${d.project.name}-pins.csv`, pinoutsCSV(d), 'text/csv', !!s.app?.canSaveFile);
    if (where) s.toast({ kind: 'ok', title: 'Pin tables saved', body: where });
  };
  return (
    <div className="col" style={{ gap: 16 }}>
      <div className="row">
        <span className="faint grow">Every pin of each part, from its datasheet's pin table, and what it's connected to. Edit connections in the Connections tab.</span>
        <button className="btn sm" onClick={exportCSV}>
          <Download size={13} /> CSV
        </button>
      </div>
      {pinouts.map((po) => {
        const pp = d.parts.find((p) => `part:${p.id}` === po.node);
        const open = pp ? () => s.open({ kind: 'part', title: pp.mpn, params: { part: pp.mpn, tab: 'pins' } }) : undefined;
        const connected = po.pins.filter((p) => p.net).length;
        return (
          <div key={po.node} className={`card${marked(hl, po.title, po.title.split(' ')[0], po.mpn) ? ' agent-hl' : ''}`}>
            <div className="row" style={{ margin: '0 0 8px', fontWeight: 600 }}>
              {open ? (
                <button className="link" onClick={open} title="Open its pin table">
                  {po.title}
                </button>
              ) : (
                po.title
              )}
              <span className="faint" style={{ fontWeight: 400, fontSize: 12 }}>
                {connected} of {po.pins.length} pins connected
              </span>
            </div>
            <table className="dtable">
              <thead>
                <tr>
                  <th>Pin</th>
                  <th>Name</th>
                  <th>Type</th>
                  <th>Net</th>
                  <th>Connected to</th>
                  <th>Description</th>
                </tr>
              </thead>
              <tbody>
                {po.pins.map((p, i) => (
                  <tr key={i} className={marked(hl, p.net) ? 'agent-hl' : undefined} style={p.net ? undefined : { opacity: 0.6 }}>
                    <td className="mono">{p.number}</td>
                    <td className="mono">{p.name}</td>
                    <td className="faint">{p.type}</td>
                    <td className="mono">{p.net ? p.net : <span className="faint">not connected</span>}</td>
                    <td>{p.netKind === 'ground' ? <span className="faint">ground</span> : p.to.join(', ')}</td>
                    <td className="muted" style={{ fontSize: 12 }}>
                      {p.description}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        );
      })}
    </div>
  );
}
