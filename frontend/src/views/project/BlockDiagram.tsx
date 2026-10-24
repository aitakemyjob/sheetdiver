import { useEffect, useMemo, useRef, useState, type PointerEvent } from 'react';
import { Download, Eye, EyeOff, LayoutGrid, Maximize2, ZoomIn, ZoomOut } from 'lucide-react';
import { api, saveText } from '../../lib/api';
import { layoutBlockDiagram, snap, type BlockDrawing, type Box, type PlacedPassive, type PlacedPin, type Pt, type Rect } from '../../lib/blockDiagram';
import { threeTerminal, twoTerminal, twoTerminalLeads, twoTerminalTransform } from '../../lib/symbols';
import { palette, swatch } from '../../lib/diagramPalette';
import { useStore } from '../../lib/store';
import type { ProjectDetail } from '../../lib/types';
import { Empty, ErrorBox } from '../../components/ui';
import { change, marked } from './connections';

/** The view's zoom (k) and pan (x, y, screen pixels). */
type View = { k: number; x: number; y: number };

/** Zooms a view by factor about a point of the screen. */
export function zoomAbout(v: View, factor: number, px: number, py: number): View {
  const k = Math.min(4, Math.max(0.15, v.k * factor));
  return { k, x: px - ((px - v.x) * k) / v.k, y: py - ((py - v.y) * k) / v.k };
}

/** Where the "connected pins only" choice is kept, for every project. */
const CONNECTED_ONLY_KEY = 'sheetdiver.diagram.connectedOnly';

/** Whether the diagram was last shown with connected pins only. */
function connectedOnlySetting(): boolean {
  try {
    return localStorage.getItem(CONNECTED_ONLY_KEY) === '1';
  } catch {
    return false;
  }
}

/** The view that fits a drawing in a screen area (never above 100%). */
export function fitView(width: number, height: number, screenW: number, screenH: number): View {
  if (!screenW) return { k: 1, x: 0, y: 0 };
  const k = Math.min(1, screenW / width, screenH / height);
  return { k, x: (screenW - width * k) / 2, y: Math.max(0, (screenH - height * k) / 2) };
}

/**
 * A pin of a symbol: its line from the body to where its wire attaches,
 * its number along the line (outside the body) and its name inside.
 * Top and bottom pins' names are written down the body. An unconnected
 * pin is dimmed.
 */
function SymbolPin({ p, body, color, stroke }: { p: PlacedPin; body: Rect; color: string; stroke: string }) {
  const edge = p.side === 'left' ? { x: body.x, y: p.y } : p.side === 'right' ? { x: body.x + body.w, y: p.y } : p.side === 'top' ? { x: p.x, y: body.y } : { x: p.x, y: body.y + body.h };
  const vertical = p.side === 'top' || p.side === 'bottom';
  // The number sits beside the pin line, just outside the body.
  const num = vertical ? { x: p.x + 3, y: p.side === 'top' ? body.y - 4 : body.y + body.h + 11, anchor: 'start' as const } : { x: (p.x + edge.x) / 2, y: p.y - 3, anchor: 'middle' as const };
  // The name is inside, along the body's edge; down the body for a top or bottom pin.
  const nameY = p.side === 'top' ? body.y + 6 : body.y + body.h - 6;
  const name = vertical
    ? { x: p.x, y: nameY, anchor: p.side === 'top' ? ('end' as const) : ('start' as const), rotate: `rotate(-90 ${p.x} ${nameY})` }
    : { x: p.side === 'left' ? body.x + 6 : body.x + body.w - 6, y: p.y, anchor: p.side === 'left' ? ('start' as const) : ('end' as const), rotate: undefined };
  return (
    <g data-pin={p.name} data-dim={p.dim || undefined} opacity={p.dim ? 0.35 : 1}>
      <path d={`M${p.x},${p.y} L${edge.x},${edge.y}`} stroke={stroke} strokeWidth={1.2} />
      {p.number && (
        <text x={num.x} y={num.y} textAnchor={num.anchor} fontSize={9} fill={color}>
          {p.number}
        </text>
      )}
      <text x={name.x} y={name.y} textAnchor={name.anchor} dominantBaseline="central" fontSize={11} fill={color} transform={name.rotate}>
        {p.name}
      </text>
    </g>
  );
}

/**
 * A two-terminal's standard symbol laid between its ends, turned round
 * when its first terminal is the symbol's second (a diode with its
 * cathode up); leads out to the ends, and its name and value beside it.
 * A clear rectangle over the node takes the pointer for dragging.
 */
function PassiveSymbol({ p, color, text, wide }: { p: PlacedPassive; color: string; text: string; wide: boolean }) {
  const q = p.passive;
  const sym = twoTerminal(q.shape);
  const flipped = p.terminals[0] === sym.terminals[1] && p.terminals[0] !== sym.terminals[0];
  const [a, z] = flipped ? [p.ends[1], p.ends[0]] : p.ends;
  const leads = twoTerminalLeads(sym, a, z);
  return (
    <g data-shape={q.shape} data-flipped={flipped || undefined}>
      <rect x={p.x} y={p.y} width={p.w} height={p.h} fill="transparent" stroke="none" />
      {leads && <path d={leads} fill="none" stroke={color} strokeWidth={wide ? 2.5 : 1.5} />}
      <g transform={twoTerminalTransform(a, z)}>
        <path d={sym.path} fill="none" stroke={color} strokeWidth={wide ? 2.5 : 1.5} strokeLinejoin="round" />
        {sym.fill && <path d={sym.fill} fill={color} stroke="none" />}
      </g>
      <text x={p.label.x} y={p.label.y} dominantBaseline="central" textAnchor={p.label.anchor} fontSize={12} fontWeight={500} fill={text}>
        {q.label}
      </text>
      {q.value && (
        <text x={p.label.x} y={p.label.y + 13} dominantBaseline="central" textAnchor={p.label.anchor} fontSize={10} fill={text}>
          {q.value}
        </text>
      )}
    </g>
  );
}

/**
 * A transistor's standard symbol in its box, its pins named at their
 * terminals (the pin table's names: G, D, S or B, C, E).
 */
function TransistorSymbol({ b, color, text, wide }: { b: Box; color: string; text: string; wide: boolean }) {
  const sym = threeTerminal(b.part.shape)!;
  return (
    <g data-shape={b.part.shape} transform={`translate(${b.x},${b.y})`}>
      <rect x={0} y={0} width={b.w} height={b.h} fill="transparent" stroke="none" />
      <path d={sym.path} fill="none" stroke={color} strokeWidth={wide ? 2.5 : 1.5} strokeLinejoin="round" />
      {sym.fill && <path d={sym.fill} fill={color} stroke="none" />}
      {b.pins.map((p, i) => {
        const t = sym.terminals[p.terminal ?? ''];
        if (!t) return null;
        const pos = t.x === 0 ? { x: 4, y: t.y - 6, anchor: 'start' as const } : t.y === 0 ? { x: t.x + 5, y: 12, anchor: 'start' as const } : { x: t.x + 5, y: t.y - 6, anchor: 'start' as const };
        return (
          <text key={i} data-pin={p.name} x={pos.x} y={pos.y} textAnchor={pos.anchor} fontSize={9} fill={text}>
            {p.name}
          </text>
        );
      })}
    </g>
  );
}

/**
 * The project's block diagram: parts and connectors with their pins,
 * wires labelled with their nets, and passives hanging off what they
 * serve. Zoom with the wheel, pan by dragging the background, drag a
 * part to move it (saved: the layout keeps it in that place and order,
 * and routes the wires again); export the whole drawing as SVG.
 */
export function BlockDiagram({ d, highlight, onChange }: { d: ProjectDetail; highlight?: string; onChange: () => void }) {
  const s = useStore();
  // A part being dragged: where it was grabbed, how far it has moved, and
  // its group's origin (positions in a group are saved relative to it).
  const [drag, setDrag] = useState<{ id: string; from: Pt; at: Pt; dx: number; dy: number; origin: Pt } | null>(null);
  const [view, setView] = useState<View | null>(null);
  const [pan, setPan] = useState<{ x: number; y: number; vx: number; vy: number } | null>(null);
  const [drawing, setDrawing] = useState<BlockDrawing | null>(null);
  // Why the last layout failed, shown instead of a stale drawing and
  // reported for the agent to see.
  const [problem, setProblem] = useState<string | null>(null);
  // A generated symbol shows every pin of its pin table; this hides the
  // unconnected ones (but supplies and grounds) on big parts.
  const [connectedOnly, setConnectedOnly] = useState(connectedOnlySetting);
  const svg = useRef<SVGSVGElement>(null);
  const content = useRef<SVGGElement>(null);
  const theme = s.app?.config.settings.theme;
  const pal = useMemo(palette, [theme]);
  // Laid out by ELK, asynchronously; a stale result is dropped.
  useEffect(() => {
    let live = true;
    layoutBlockDiagram(d.connections.diagram, { connectedOnly })
      .then((r) => {
        if (!live) return;
        setDrawing(r);
        setProblem(null);
        // Where each part went, so the agent can check its placement.
        api.reportDrawn({ project: d.project.name, cells: r.cells }).catch(() => {});
      })
      .catch((e: unknown) => {
        if (!live) return;
        const message = e instanceof Error ? e.message : String(e);
        setProblem(message);
        api.uiContext({ view: 'project/diagram', project: d.project.name, problem: `the block diagram failed to lay out: ${message}` }).catch(() => {});
      });
    return () => {
      live = false;
    };
  }, [d, connectedOnly]);
  const toggleConnectedOnly = () => {
    const next = !connectedOnly;
    setConnectedOnly(next);
    try {
      localStorage.setItem(CONNECTED_ONLY_KEY, next ? '1' : '0');
    } catch {
      // Nothing to remember it in: it still applies for now.
    }
  };
  const hasFull = d.connections.diagram.parts.some((p) => p.full);
  const height = drawing?.height ?? 0;
  const empty = d.connections.diagram.parts.length === 0;
  const fit = () => setView(fitView(drawing?.width ?? 0, height, svg.current?.clientWidth ?? 0, svg.current?.clientHeight ?? 0));
  const zoom = (factor: number, px?: number, py?: number) =>
    setView((v) => zoomAbout(v ?? { k: 1, x: 0, y: 0 }, factor, px ?? (svg.current?.clientWidth ?? 0) / 2, py ?? (svg.current?.clientHeight ?? 0) / 2));
  // Fitted when first drawn (the wheel listener needs the drawing too).
  const drawn = !!drawing;
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => fit(), [drawn]);
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
  }, [drawn]);
  if (empty) {
    return (
      <Empty icon={<LayoutGrid size={28} />} title="Nothing connected yet">
        Put pins on nets in the Connections tab, or ask your agent to draft the block diagram (set_connections).
      </Empty>
    );
  }
  if (problem) return <ErrorBox error={`The block diagram couldn't be laid out: ${problem}. Please report this with the connection table (Connections tab).`} />;
  if (!drawing) return null;
  // Screen to drawing coordinates, through the zoom and pan.
  const at = (e: PointerEvent) => {
    const m = content.current?.getScreenCTM?.();
    return m && typeof DOMPoint !== 'undefined' ? new DOMPoint(e.clientX, e.clientY).matrixTransform(m.inverse()) : { x: e.clientX, y: e.clientY };
  };
  const grab = (e: PointerEvent, id: string, r: Pt & { origin?: Pt }) => {
    const p = at(e);
    (e.target as Element).setPointerCapture?.(e.pointerId);
    setDrag({ id, from: p, at: { x: r.x, y: r.y }, dx: 0, dy: 0, origin: r.origin ?? { x: 0, y: 0 } });
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
      // In grid steps: what's dragged stays on the grid, and so does where it's saved.
      setDrag({ ...drag, dx: snap(p.x - drag.from.x), dy: snap(p.y - drag.from.y) });
    }
  };
  const drop = () => {
    setPan(null);
    if (!drag) return;
    setDrag(null);
    if (!drag.dx && !drag.dy) return;
    const pos = { x: Math.max(0, snap(drag.at.x + drag.dx - drag.origin.x)), y: Math.max(0, snap(drag.at.y + drag.dy - drag.origin.y)) };
    change(d.project.name, { positions: [{ node: drag.id, ...pos }] }, onChange);
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
  // What's being dragged follows the pointer until it's dropped and laid out again.
  const dragging = (id: string) => (drag?.id === id ? `translate(${drag.dx},${drag.dy})` : undefined);
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
        {hasFull && (
          <button
            className="btn sm"
            aria-pressed={connectedOnly}
            onClick={toggleConnectedOnly}
            title={connectedOnly ? "Show every pin of each part's pin table" : 'Hide the pins nothing is connected to, except supplies and grounds'}
          >
            {connectedOnly ? <EyeOff size={13} /> : <Eye size={13} />} {connectedOnly ? 'Connected pins only' : 'All pins'}
          </button>
        )}
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
            {drawing.clusters.map((c, i) => (
              // A subgroup: its parts wired up as one unit, in a faint frame named only when it was named.
              <g key={`c${i}`} data-cluster={c.name}>
                <rect x={c.x} y={c.y} width={c.w} height={c.h} rx={6} fill="none" stroke={pal.group.stroke} strokeWidth={1} strokeDasharray="2 3" />
                {c.name && (
                  <text x={c.x + 8} y={c.y + 14} fontSize={11} fontWeight={600} fill={pal.group.text}>
                    {c.name}
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
                  transform={dragging(b.part.id)}
                  style={{ cursor: drag?.id === b.part.id ? 'grabbing' : 'grab' }}
                >
                  <text x={b.x + b.w / 2} y={b.y - 12} textAnchor="middle" fontSize={14} fontWeight={600} fill={c.text}>
                    {b.part.title}
                  </text>
                  {threeTerminal(b.part.shape) ? (
                    <TransistorSymbol b={b} color={on ? 'var(--accent)' : c.stroke} text={c.text} wide={on} />
                  ) : (
                    <>
                      <rect x={b.body.x} y={b.body.y} width={b.body.w} height={b.body.h} rx={3} fill={c.fill} stroke={on ? 'var(--accent)' : c.stroke} strokeWidth={on ? 2.5 : 0.9} />
                      {b.pins.map((p, i) => (
                        <SymbolPin key={i} p={p} body={b.body} color={c.text} stroke={c.stroke} />
                      ))}
                    </>
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
                  transform={dragging(q.id.split(',')[0])}
                  style={{ cursor: drag?.id === q.id.split(',')[0] ? 'grabbing' : 'grab' }}
                >
                  <PassiveSymbol p={p} color={on ? 'var(--accent)' : c.stroke} text={c.text} wide={on} />
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
