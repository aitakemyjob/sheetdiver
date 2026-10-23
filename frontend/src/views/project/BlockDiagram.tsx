import { useEffect, useMemo, useRef, useState, type PointerEvent } from 'react';
import { Download, LayoutGrid, Maximize2, ZoomIn, ZoomOut } from 'lucide-react';
import { saveText } from '../../lib/api';
import { layoutBlockDiagram, type Box, type Pt } from '../../lib/blockDiagram';
import { palette, swatch } from '../../lib/diagramPalette';
import { useStore } from '../../lib/store';
import type { ProjectDetail } from '../../lib/types';
import { Empty } from '../../components/ui';
import { change, marked } from './connections';

/** The view's zoom (k) and pan (x, y, screen pixels). */
type View = { k: number; x: number; y: number };

/** Zooms a view by factor about a point of the screen. */
export function zoomAbout(v: View, factor: number, px: number, py: number): View {
  const k = Math.min(4, Math.max(0.15, v.k * factor));
  return { k, x: px - ((px - v.x) * k) / v.k, y: py - ((py - v.y) * k) / v.k };
}

/** The view that fits a drawing in a screen area (never above 100%). */
export function fitView(width: number, height: number, screenW: number, screenH: number): View {
  if (!screenW) return { k: 1, x: 0, y: 0 };
  const k = Math.min(1, screenW / width, screenH / height);
  return { k, x: (screenW - width * k) / 2, y: Math.max(0, (screenH - height * k) / 2) };
}

/**
 * The project's block diagram: parts and connectors with their pins,
 * wires labelled with their nets, and passives hanging off what they
 * serve. Zoom with the wheel, pan by dragging the background, drag
 * parts to move them (saved); export the whole drawing as SVG.
 */
export function BlockDiagram({ d, highlight, onChange }: { d: ProjectDetail; highlight?: string; onChange: () => void }) {
  const s = useStore();
  const [moved, setMoved] = useState<Record<string, { x: number; y: number }>>({});
  // Positions in a group are saved relative to its frame (origin).
  const [drag, setDrag] = useState<{ id: string; dx: number; dy: number; origin: Pt } | null>(null);
  const [view, setView] = useState<View | null>(null);
  const [pan, setPan] = useState<{ x: number; y: number; vx: number; vy: number } | null>(null);
  const svg = useRef<SVGSVGElement>(null);
  const content = useRef<SVGGElement>(null);
  useEffect(() => setMoved({}), [d]);
  const theme = s.app?.config.settings.theme;
  const pal = useMemo(palette, [theme]);
  const drawing = useMemo(() => {
    const dg = d.connections.diagram;
    // Parts by their node; passives by their first one's.
    return layoutBlockDiagram({ ...dg, parts: dg.parts.map((p) => ({ ...p, ...moved[p.id] })), passives: dg.passives.map((q) => ({ ...q, ...moved[q.id.split(',')[0]] })) });
  }, [d, moved]);
  const height = drawing.height;
  const empty = drawing.boxes.length === 0;
  const fit = () => setView(fitView(drawing.width, height, svg.current?.clientWidth ?? 0, svg.current?.clientHeight ?? 0));
  const zoom = (factor: number, px?: number, py?: number) =>
    setView((v) => zoomAbout(v ?? { k: 1, x: 0, y: 0 }, factor, px ?? (svg.current?.clientWidth ?? 0) / 2, py ?? (svg.current?.clientHeight ?? 0) / 2));
  // Fitted when first shown.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => fit(), [empty]);
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
  }, [empty]);
  if (empty) {
    return (
      <Empty icon={<LayoutGrid size={28} />} title="Nothing connected yet">
        Put pins on nets in the Connections tab, or ask your agent to draft the block diagram (set_connections).
      </Empty>
    );
  }
  // Screen to drawing coordinates, through the zoom and pan.
  const at = (e: PointerEvent) => {
    const m = content.current?.getScreenCTM?.();
    return m && typeof DOMPoint !== 'undefined' ? new DOMPoint(e.clientX, e.clientY).matrixTransform(m.inverse()) : { x: e.clientX, y: e.clientY };
  };
  const grab = (e: PointerEvent, id: string, r: Pt & { origin?: Pt }) => {
    const p = at(e);
    (e.target as Element).setPointerCapture?.(e.pointerId);
    setDrag({ id, dx: p.x - r.x, dy: p.y - r.y, origin: r.origin ?? { x: 0, y: 0 } });
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
    } else if (drag) {
      const p = at(e);
      const pos = { x: Math.max(0, Math.round(p.x - drag.dx - drag.origin.x)), y: Math.max(0, Math.round(p.y - drag.dy - drag.origin.y)) };
      setMoved((m) => ({ ...m, [drag.id]: pos }));
    }
  };
  const drop = () => {
    setPan(null);
    if (!drag) return;
    const pos = moved[drag.id];
    setDrag(null);
    if (pos) change(d.project.name, { positions: [{ node: drag.id, ...pos }] }, onChange);
  };
  const open = (b: Box) => {
    // A designator of an entry with several is "part:<id>/R1".
    const pp = d.parts.find((p) => `part:${p.id}` === b.part.id.split('/')[0]);
    if (pp) s.open({ kind: 'part', title: pp.mpn, params: { part: pp.mpn } });
  };
  const exportSVG = async () => {
    if (!svg.current) return;
    // The whole drawing at full size, whatever the view.
    const copy = svg.current.cloneNode(true) as SVGSVGElement;
    copy.setAttribute('width', String(drawing.width));
    copy.setAttribute('height', String(height));
    copy.setAttribute('viewBox', `0 0 ${drawing.width} ${height}`);
    copy.removeAttribute('style');
    copy.querySelector('g[data-view]')?.removeAttribute('transform');
    const css = getComputedStyle(document.documentElement);
    const text = copy.outerHTML.replace(/var\((--[\w-]+)\)/g, (_, v: string) => css.getPropertyValue(v).trim() || 'sans-serif');
    const where = await saveText(`${d.project.name}-block-diagram.svg`, `<?xml version="1.0" encoding="UTF-8"?>\n${text}`, 'image/svg+xml', !!s.app?.canSaveFile);
    if (where) s.toast({ kind: 'ok', title: 'Block diagram saved', body: where });
  };
  const hl = highlight?.toLowerCase();
  const w = pal.wire;
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
          onPointerUp={drop}
          style={{ display: 'block', userSelect: 'none', height: 'max(380px, calc(100vh - 340px))', cursor: pan ? 'grabbing' : 'default', touchAction: 'none', fontFamily: 'var(--font-ui)' }}
        >
          <defs>
            <marker id="sd-arrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
              <path d="M2 1L8 5L2 9" fill="none" stroke={w.stroke} strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
            </marker>
          </defs>
          <g ref={content} data-view transform={`translate(${view?.x ?? 0},${view?.y ?? 0}) scale(${view?.k ?? 1})`}>
            {drawing.frames.map((f) => (
              <g key={f.group.name} data-group={f.group.name}>
                <rect x={f.x} y={f.y} width={f.w} height={f.h} rx={8} fill={pal.group.fill} stroke={pal.group.stroke} strokeWidth={1} strokeDasharray="6 4" />
                <text x={f.x + 14} y={f.y + 22} fontSize={15} fontWeight={700} fill={pal.group.text}>
                  {f.group.name}
                </text>
                {f.group.description && (
                  <text x={f.x + 14} y={f.y + 42} fontSize={12} fill={pal.group.text}>
                    {f.group.description}
                  </text>
                )}
                {f.group.notes && (
                  <text x={f.x + 14} y={f.y + (f.group.description ? 62 : 42)} fontSize={12} fontStyle="italic" fill={pal.group.text}>
                    Notes: {f.group.notes}
                  </text>
                )}
              </g>
            ))}
            {drawing.wires.map((x, i) => {
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
            {drawing.boxes.map((b) => {
              const c = swatch(pal, b.part.kind);
              const on = marked(hl, b.part.title, b.part.title.split(' ')[0], b.part.mpn);
              return (
                <g
                  key={b.part.id}
                  data-part={b.part.title}
                  data-hl={on || undefined}
                  onPointerDown={(e) => grab(e, b.part.id, b)}
                  onDoubleClick={() => open(b)}
                  style={{ cursor: drag?.id === b.part.id ? 'grabbing' : 'grab' }}
                >
                  <text x={b.x + b.w / 2} y={b.y - 12} textAnchor="middle" fontSize={14} fontWeight={600} fill={c.text}>
                    {b.part.title}
                  </text>
                  <rect x={b.x} y={b.y} width={b.w} height={b.h} rx={4} fill={c.fill} stroke={on ? 'var(--accent)' : c.stroke} strokeWidth={on ? 2.5 : 0.8} />
                  {b.pins.map((p, i) =>
                    p.side === 'bottom' ? (
                      <text key={i} x={p.x} y={b.y + b.h - 12} textAnchor="middle" fontSize={12} fill={c.text}>
                        {p.name}
                      </text>
                    ) : (
                      <text key={i} x={p.side === 'right' ? p.x - 12 : p.x + 12} y={p.y} dominantBaseline="central" textAnchor={p.side === 'right' ? 'end' : 'start'} fontSize={12} fill={c.text}>
                        {p.tied && p.side === 'inside' ? `${p.name}→${p.tied}` : p.name}
                      </text>
                    ),
                  )}
                </g>
              );
            })}
            {drawing.passives.map((p) => {
              const q = p.passive;
              const c = swatch(pal, q.kind);
              const on = marked(hl, ...q.label.split(','));
              return (
                <g
                  key={q.id}
                  data-passive={q.label}
                  data-hl={on || undefined}
                  onPointerDown={(e) => grab(e, q.id.split(',')[0], p)}
                  style={{ cursor: drag?.id === q.id.split(',')[0] ? 'grabbing' : 'grab' }}
                >
                  <rect x={p.x} y={p.y} width={p.w} height={p.h} rx={4} fill={c.fill} stroke={on ? 'var(--accent)' : c.stroke} strokeWidth={on ? 2.5 : 0.8} />
                  <text x={p.x + p.w / 2} y={p.y + (q.value ? 15 : p.h / 2)} dominantBaseline="central" textAnchor="middle" fontSize={13} fontWeight={500} fill={c.text}>
                    {q.label}
                  </text>
                  {q.value && (
                    <text x={p.x + p.w / 2} y={p.y + 30} dominantBaseline="central" textAnchor="middle" fontSize={11} fill={c.text}>
                      {q.value}
                    </text>
                  )}
                </g>
              );
            })}
            {drawing.grounds.map((g, i) => (
              // A small ground symbol: a stub and three bars.
              <path
                key={`g${i}`}
                data-ground
                d={`M${g.x},${g.y} V${g.y + 4} M${g.x - 6},${g.y + 4} H${g.x + 6} M${g.x - 4},${g.y + 7} H${g.x + 4} M${g.x - 2},${g.y + 10} H${g.x + 2}`}
                fill="none"
                stroke={w.stroke}
                strokeWidth={1.2}
              />
            ))}
            {drawing.flags.map((f, i) => (
              // The net at a passive's far end (a pull-up's 3V3): a bar and its name.
              <g key={`f${i}`} data-flag={f.text}>
                <path d={`M${f.x - 6},${f.y} H${f.x + 6}`} stroke={w.stroke} strokeWidth={1.4} />
                <text x={f.x} y={f.up ? f.y - 4 : f.y + 12} textAnchor="middle" fontSize={10} fill={w.text}>
                  {f.text}
                </text>
              </g>
            ))}
            {drawing.dots.map((p, i) => (
              <circle key={`j${i}`} data-junction cx={p.x} cy={p.y} r={2.5} fill={w.stroke} />
            ))}
          </g>
        </svg>
      </div>
    </div>
  );
}
