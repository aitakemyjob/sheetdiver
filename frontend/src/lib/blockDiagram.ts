// Places a project's block diagram. The backend decides what is drawn
// (core.Diagram: each box's pins and their sides, how each net's wire
// runs, where each passive hangs); this decides only where things go, in
// stages that each do one thing:
//
//   measure → each box's size from its pins
//   place   → boxes in columns, stacked, saved positions, no overlaps
//   pinsAt  → where each pin is on its box
//   route   → each net's wire: buses from a net's driver to the parts it
//             feeds, wires between two boxes, channels under a column,
//             stubs leaving a box; ground pins to ground symbols
//   hang    → each passive where nothing it adds crosses or covers
//             another net: a bridge across its two pins, the others on a
//             drop from their wire or pin, each to a ground symbol
//
// What it promises, for any board (tested on several): nothing overlaps,
// no wire crosses another net's wire, nothing sits on another net's wire.
//
// A board in functional groups is laid out a group at a time (groups
// share no wires: a net between groups is a labelled stub in each), each
// framed with its name, description and notes, left to right in power
// flow order. Saved positions in a group are relative to its frame.
import type { Diagram, DiagramGroup, DiagramPart, DiagramPassive, DiagramPin } from './types';

export const PITCH = 30; // between pins
export const TOP = 30; // first pin below a box's top
export const BOX_W = 200;
export const PAD = 40;
const TITLE = 30; // a box's title above it
const TRACK = 12; // between wires turning in the same gap
const DROP = 40; // from a wire down to a passive hanging from it
const GROUND = 16; // a ground symbol's height, with its stub
const SLOT = 84; // between bottom pins
/** The room a ground pin's name needs along the bottom. */
const groundSlot = (p: DiagramPin) => Math.max(44, p.name.length * 7 + 16);

/** A point. */
export interface Pt {
  x: number;
  y: number;
}

/** A rectangle. */
export interface Rect extends Pt {
  w: number;
  h: number;
}

/** A pin with its place on the drawing. */
export type PlacedPin = DiagramPin & Pt;

/** A box with its place and its pins'. */
export interface Box extends Rect {
  part: DiagramPart;
  pins: PlacedPin[];
  /** Where its group's content starts: saved positions are relative to it. */
  origin?: Pt;
}

/** A wire (an SVG path) with an optional net label. */
export interface Wire {
  net: string;
  path: string;
  label?: { x: number; y: number; anchor: 'start' | 'middle' | 'end'; text: string };
  arrow?: boolean;
  /** A passive's or ground pin's own short wire. */
  thin?: boolean;
}

/** A passive with its place. */
export interface PlacedPassive extends Rect {
  passive: DiagramPassive;
  /** Where its group's content starts: saved positions are relative to it. */
  origin?: Pt;
}

/** A group's frame, with what's written at its top. */
export interface Frame extends Rect {
  group: DiagramGroup;
  /** Height of the header with its name, description and notes. */
  header: number;
}

export interface BlockDrawing {
  boxes: Box[];
  passives: PlacedPassive[];
  wires: Wire[];
  /** Ground symbols, by the top of their stub. */
  grounds: Pt[];
  /** Net names at the far end of pull-ups and the like. */
  flags: Flag[];
  /** Junctions: where a drop leaves a wire. */
  dots: Pt[];
  /** Group frames, behind everything else. */
  frames: Frame[];
  width: number;
  height: number;
}

/** Lays out a block diagram: whole, or a group at a time. */
export function layoutBlockDiagram(d: Diagram): BlockDrawing {
  return d.groups?.length ? layoutGroups(d) : layoutWhole(d);
}

/** Lays out a block diagram (or one group of it) as one drawing. */
function layoutWhole(d: Diagram): BlockDrawing {
  const boxes = measure(d);
  place(boxes, d);
  facing(boxes, d);
  boxes.forEach(pinsAt);
  const r = route(d, boxes);
  const h = hang(d, boxes, r);
  const wires = [...r.wires, ...h.wires];
  const extents = [
    ...boxes.map((b) => ({ x: b.x + b.w, y: b.y + b.h })),
    ...h.passives.map((p) => ({ x: p.x + p.w, y: p.y + p.h + GROUND })),
    ...[...r.grounds, ...h.grounds].map((g) => ({ x: g.x + 8, y: g.y + GROUND })),
    ...wires.map(wireExtent),
  ];
  return {
    boxes,
    passives: h.passives,
    wires,
    grounds: [...r.grounds, ...h.grounds],
    flags: h.flags,
    dots: [...r.dots, ...h.dots],
    frames: [],
    width: Math.max(320, ...extents.map((e) => e.x)) + PAD,
    height: Math.max(160, ...extents.map((e) => e.y)) + PAD,
  };
}

// ── groups ────────────────────────────────────────────────────────────

const GROUP_GAP = 60; // between group frames
const HEADER_LINE = 20; // a description or notes line in a frame's header

/** A frame's header height: its name, then its description and notes. */
export function headerHeight(g: DiagramGroup): number {
  return 34 + (g.description ? HEADER_LINE : 0) + (g.notes ? HEADER_LINE : 0);
}

/** The part of a diagram in one group ("" for none), its columns renumbered from 0. */
export function groupDiagram(d: Diagram, group: string): Diagram {
  const inGroup = (g?: string) => (g ?? '').toLowerCase() === group.toLowerCase();
  const parts = d.parts.filter((p) => inGroup(p.group));
  const columns = [...new Set(parts.map((p) => p.column))].sort((a, b) => a - b);
  return {
    parts: parts.map((p) => ({ ...p, column: columns.indexOf(p.column) })),
    nets: d.nets.filter((n) => inGroup(n.group)),
    passives: d.passives.filter((q) => inGroup(q.group)),
    groups: [],
    links: [],
  };
}

/** Moves an SVG path of absolute M, L, H and V commands. */
export function movePath(path: string, dx: number, dy: number): string {
  return path.replace(/([MLHV])(-?[\d.]+)(?:,(-?[\d.]+))?/g, (_, cmd: string, a: string, b?: string) => {
    if (cmd === 'H') return `H${Number(a) + dx}`;
    if (cmd === 'V') return `V${Number(a) + dy}`;
    return `${cmd}${Number(a) + dx},${Number(b) + dy}`;
  });
}

/** The top-left corner of everything drawn: boxes with their titles, passives, wires and their labels, symbols. */
export function topLeft(r: BlockDrawing): Pt {
  const pts: Pt[] = [
    ...r.boxes.map((b) => ({ x: b.x, y: b.y - TITLE })),
    ...r.passives,
    ...r.grounds,
    ...r.flags.map((f) => ({ x: f.x - f.text.length * 3.6, y: f.y - 12 })),
    ...r.dots,
    ...r.wires.flatMap((w) => [
      ...[...w.path.matchAll(/([MLHV])(-?[\d.]+)(?:,(-?[\d.]+))?/g)].map(([, c, a, b]) => (c === 'V' ? { x: Infinity, y: Number(a) } : { x: Number(a), y: c === 'H' ? Infinity : Number(b) })),
      ...(w.label ? [labelRect(w.label)] : []),
    ]),
  ];
  return { x: Math.min(...pts.map((p) => p.x)), y: Math.min(...pts.map((p) => p.y)) };
}

/** Moves a whole drawing by (dx, dy). */
export function moveDrawing(r: BlockDrawing, dx: number, dy: number): BlockDrawing {
  const pt = <T extends Pt>(p: T): T => ({ ...p, x: p.x + dx, y: p.y + dy });
  return {
    ...r,
    boxes: r.boxes.map((b) => ({ ...pt(b), pins: b.pins.map(pt) })),
    passives: r.passives.map(pt),
    wires: r.wires.map((w) => ({ ...w, path: movePath(w.path, dx, dy), label: w.label && pt(w.label) })),
    grounds: r.grounds.map(pt),
    flags: r.flags.map(pt),
    dots: r.dots.map(pt),
    frames: r.frames.map(pt),
  };
}

/**
 * Lays out each group on its own, framed, left to right in the backend's
 * order, then the parts in no group. Saved positions are relative to
 * their group's content, so moving a part in one group never moves
 * another group's parts.
 */
function layoutGroups(d: Diagram): BlockDrawing {
  const out: BlockDrawing = { boxes: [], passives: [], wires: [], grounds: [], flags: [], dots: [], frames: [], width: 0, height: 0 };
  let x = 0;
  const clusters: (DiagramGroup | undefined)[] = [...d.groups, undefined];
  for (const group of clusters) {
    const sub = groupDiagram(d, group?.name ?? '');
    if (!sub.parts.length) continue;
    const header = group ? headerHeight(group) : 0;
    // What's drawn starts a margin inside the frame, however far its stubs
    // and passives reach out from the boxes.
    const drawn = layoutWhole(sub);
    const corner = topLeft(drawn);
    const dx = x + PAD - Math.min(PAD, corner.x);
    const dy = header + PAD - Math.min(PAD, corner.y);
    const origin = { x: dx, y: dy };
    const local = moveDrawing(drawn, dx, dy);
    local.width = drawn.width + (dx - x);
    local.height = drawn.height + (dy - header);
    // Boxes keep their own column (the group's layout renumbers them).
    const column = new Map(d.parts.map((p) => [p.id, p.column]));
    local.boxes = local.boxes.map((b) => ({ ...b, part: { ...b.part, column: column.get(b.part.id) ?? b.part.column } }));
    // The frame fits what's drawn, and the header's text.
    const textWidth = group ? 24 + 7.2 * Math.max(group.name.length * 1.15, (group.description ?? '').length, (group.notes ?? '').length + 7) : 0;
    const w = Math.max(local.width, textWidth);
    if (group) out.frames.push({ x: x + 12, y: 12, w: w - 24, h: header + local.height - 24, group, header });
    out.boxes.push(...local.boxes.map((b) => ({ ...b, origin })));
    out.passives.push(...local.passives.map((p) => ({ ...p, origin })));
    out.wires.push(...local.wires);
    out.grounds.push(...local.grounds);
    out.flags.push(...local.flags);
    out.dots.push(...local.dots);
    out.width = x + w;
    out.height = Math.max(out.height, header + local.height);
    x += w + GROUP_GAP;
  }
  return out;
}

// ── measure ───────────────────────────────────────────────────────────

/** Sizes each box: tall enough for its pins, wide enough for its bottom pins. */
export function measure(d: Diagram): Box[] {
  return d.parts.map((part) => {
    const pins = part.pins.map((p) => ({ ...p, x: 0, y: 0 }));
    // A pin tied to another of the part's pins (EN to VIN) is drawn on
    // the same side, just below it, joined to its wire.
    for (const p of pins.filter((x) => x.side === 'inside' && x.tied)) {
      const to = pins.find((x) => x.name === p.tied && x.side !== 'inside' && !x.ground);
      if (to && (to.side === 'left' || to.side === 'right')) p.side = to.side;
    }
    const left = pins.filter((p) => p.side === 'left' || p.side === 'inside').length;
    const right = pins.filter((p) => p.side === 'right').length;
    const bottom = pins.filter((p) => p.side === 'bottom' && !p.ground).length;
    const grounds = pins.filter((p) => p.ground);
    const w = Math.max(BOX_W, 24 + bottom * SLOT + grounds.reduce((n, p) => n + groundSlot(p), 0) + 24);
    const h = Math.max(120, TOP + Math.max(left, right) * PITCH + (bottom || grounds.length ? 44 : 10));
    return { part, pins, x: 0, y: 0, w, h };
  });
}

// ── place ─────────────────────────────────────────────────────────────

/** How far out from a box's side its stubs and bridged passives reach. */
function reach(d: Diagram, b: Box, side: 'left' | 'right'): number {
  const out = d.nets.filter((n) => n.wire === 'out' && b.part.pins.some((p) => p.side === side && p.net === n.name));
  const drops = out.reduce((n, net) => n + d.passives.filter((q) => q.mount === 'out' && q.nets[0] === net.name).length, 0);
  const bridged = side === 'right' && d.passives.some((q) => q.part === b.part.id && q.mount === 'bridge') ? 120 : 0;
  return bridged + out.length * 30 + drops * 80;
}

/** The parts each part shares a net with (ground aside). */
function neighbours(d: Diagram): Map<string, string[]> {
  const out = new Map<string, string[]>();
  for (const n of d.nets)
    for (const a of n.parts)
      for (const b of n.parts) if (a !== b) out.set(a, [...(out.get(a) ?? []), b]);
  return out;
}

/**
 * Orders the boxes in each column so wires between columns don't swap:
 * a few sweeps that sort each column by the mean place of the boxes it
 * connects to in the column before (then after).
 */
export function orderColumns(cols: Box[][], d: Diagram): void {
  const near = neighbours(d);
  const rank = new Map<string, number>();
  const index = () => cols.forEach((c) => c.forEach((b, i) => rank.set(b.part.id, i)));
  index();
  for (let sweep = 0; sweep < 4; sweep++) {
    const order = sweep % 2 === 0 ? cols.map((_, i) => i) : cols.map((_, i) => cols.length - 1 - i);
    for (const c of order) {
      const side = sweep % 2 === 0 ? c - 1 : c + 1;
      const there = new Set((cols[side] ?? []).map((b) => b.part.id));
      const centre = (b: Box) => {
        const ns = (near.get(b.part.id) ?? []).filter((n) => there.has(n));
        return ns.length ? ns.reduce((s, n) => s + (rank.get(n) ?? 0), 0) / ns.length : (rank.get(b.part.id) ?? 0);
      };
      cols[c]?.sort((a, b) => centre(a) - centre(b));
      index();
    }
  }
}

/**
 * Places boxes in their columns, ordered by what they connect to and
 * stacked. A gap between columns is as wide as what's in it needs: stubs,
 * passives hanging from wires, and the wires turning in it. Saved
 * positions win; overlapping boxes are then moved apart.
 */
export function place(boxes: Box[], d: Diagram): void {
  const cols: Box[][] = [];
  for (const b of boxes) (cols[b.part.column] ??= []).push(b);
  for (let i = 0; i < cols.length; i++) cols[i] ??= [];
  orderColumns(cols, d);
  const top = PAD + TITLE + 10;
  const turning = d.nets.filter((n) => n.wire === 'between' || n.wire === 'lane').length;
  let x = PAD + 70; // room for an input wire on the left
  for (let c = 0; c < cols.length; c++) {
    const list = cols[c];
    const leftReach = Math.max(0, ...list.map((o) => reach(d, o, 'left')));
    let y = top;
    for (const b of list) {
      b.x = b.part.x ?? x + leftReach;
      b.y = b.part.y ?? y;
      y += b.h + 70 + (b.pins.some((p) => p.side === 'bottom' && !p.ground) ? 110 : GROUND + 20);
    }
    const right = Math.max(0, ...list.map((b) => reach(d, b, 'right')));
    const left = Math.max(0, ...(cols[c + 1] ?? []).map((b) => reach(d, b, 'left')));
    // Passives on a wire between two parts hang in the gap the wire
    // crosses: after its left part, whichever part they serve.
    const leftEnd = (q: DiagramPassive) => d.nets.find((n) => n.name === q.nets[0] && n.wire === 'between' && n.parts.includes(q.part))?.parts[0] ?? q.part;
    const between = d.passives.filter((q) => q.mount === 'wire' && list.some((b) => b.part.id === leftEnd(q))).length;
    x += leftReach + Math.max(BOX_W, ...list.map((b) => b.w)) + Math.max(160, right + left + between * 80 + turning * 12 + 90);
  }
  separate(boxes);
}

/**
 * Orders the pins on each side of each box by where their wires go (the
 * height of the part at the other end), keeping the backend's grouping:
 * wires to other parts, then stubs leaving the board, then bridged pins.
 */
export function facing(boxes: Box[], d: Diagram): void {
  const at = new Map(boxes.map((b) => [b.part.id, b.y + b.h / 2]));
  const nets = new Map(d.nets.map((n) => [n.name, n]));
  for (const b of boxes) {
    const group = (p: PlacedPin) => (p.ground ? 4 : p.bridged ? 3 : (nets.get(p.net)?.parts.length ?? 0) > 1 ? 1 : 2);
    const other = (p: PlacedPin) => {
      const ys = (nets.get(p.net)?.parts ?? []).filter((id) => id !== b.part.id).map((id) => at.get(id) ?? 0);
      return ys.length ? Math.min(...ys) : 0;
    };
    const order = new Map(b.pins.map((p, i) => [p, i]));
    const sorted = (side: DiagramPin['side']) =>
      b.pins.filter((p) => p.side === side).sort((p, q) => group(p) - group(q) || other(p) - other(q) || order.get(p)! - order.get(q)!);
    b.pins = (['left', 'right', 'bottom', 'inside'] as const).flatMap(sorted);
  }
}

/** Whether two rectangles overlap, with a margin. */
export function overlaps(a: Rect, b: Rect, margin = 0): boolean {
  return a.x < b.x + b.w + margin && b.x < a.x + a.w + margin && a.y < b.y + b.h + margin && b.y < a.y + a.h + margin;
}

/** A box with its title above and room for its ground symbols below. */
const footprint = (b: Rect): Rect => ({ x: b.x, y: b.y - TITLE, w: b.w, h: b.h + TITLE + GROUND });

/**
 * Moves boxes that overlap (with their titles) down until none do: a
 * part dragged onto another, or positions saved from an older layout.
 */
export function separate(boxes: Box[]): void {
  const order = [...boxes].sort((a, b) => a.y - b.y || a.x - b.x);
  for (let round = 0; round < order.length * order.length; round++) {
    let moved = false;
    for (let i = 0; i < order.length; i++)
      for (let j = i + 1; j < order.length; j++) {
        if (overlaps(footprint(order[i]), footprint(order[j]), 24)) {
          order[j].y = order[i].y + order[i].h + 70 + GROUND;
          moved = true;
        }
      }
    if (!moved) return;
  }
}

// ── pinsAt ────────────────────────────────────────────────────────────

/**
 * Puts each pin on its side, in the backend's order: left (then inside
 * ones below), right, bottom (ground pins last, closer together).
 */
export function pinsAt(b: Box): void {
  // Each pin, then the pins tied to it, so a tie spans one pitch.
  const withTies = (side: 'left' | 'right') => {
    const own = b.pins.filter((p) => p.side === side && !p.tied);
    const tied = b.pins.filter((p) => p.side === side && p.tied);
    const rest = tied.filter((t) => !own.some((p) => p.name === t.tied));
    return [...own.flatMap((p) => [p, ...tied.filter((t) => t.tied === p.name)]), ...rest];
  };
  [...withTies('left'), ...b.pins.filter((p) => p.side === 'inside')].forEach((p, i) => Object.assign(p, { x: b.x, y: b.y + TOP + i * PITCH }));
  withTies('right').forEach((p, i) => Object.assign(p, { x: b.x + b.w, y: b.y + TOP + i * PITCH }));
  const bottom = b.pins.filter((p) => p.side === 'bottom' && !p.ground);
  bottom.forEach((p, i) => Object.assign(p, { x: b.x + 50 + i * SLOT, y: b.y + b.h }));
  // Ground pins from the right, each as wide as its name.
  let x = b.x + b.w - 12;
  for (const p of b.pins.filter((g) => g.ground).reverse()) {
    x -= groundSlot(p) / 2;
    Object.assign(p, { x, y: b.y + b.h });
    x -= groundSlot(p) / 2;
  }
}

// ── route ─────────────────────────────────────────────────────────────

/** A straight piece of a net's wire (horizontal or vertical). */
export interface Seg {
  net: string;
  x1: number;
  y1: number;
  x2: number;
  y2: number;
}

/** A piece of wire passives can hang from. */
interface Trunk {
  net: string;
  x1: number;
  x2: number;
  y: number;
  /** A stub that can be made longer when its passives need room. */
  stub?: { wire: Wire; pin: PlacedPin; dir: 1 | -1 };
}

export interface Routed {
  wires: Wire[];
  grounds: Pt[];
  /** Junctions where a bus branches. */
  dots: Pt[];
  /** Where each net's passives can hang. */
  trunks: Map<string, Trunk[]>;
}

/** The pin of a box on a net. */
export function pinOn(boxes: Box[], part: string, net: string): PlacedPin | undefined {
  return boxes.find((b) => b.part.id === part)?.pins.find((p) => p.side !== 'inside' && !p.ground && !p.tied && p.net === net);
}

/** The pieces of a path of M, H, V and L commands. */
export function pieces(net: string, path: string): Seg[] {
  const out: Seg[] = [];
  let x = 0;
  let y = 0;
  let first = true;
  for (const [, cmd, a, b] of path.matchAll(/([MLHV])(-?[\d.]+)(?:,(-?[\d.]+))?/g)) {
    const [px, py] = [x, y];
    if (cmd === 'M' || cmd === 'L') [x, y] = [Number(a), Number(b)];
    if (cmd === 'H') x = Number(a);
    if (cmd === 'V') y = Number(a);
    if (!first && cmd !== 'M') out.push({ net, x1: Math.min(px, x), y1: Math.min(py, y), x2: Math.max(px, x), y2: Math.max(py, y) });
    first = false;
  }
  return out;
}

/** A wire to route through one gap between columns, from a pin to one or more pins. */
interface Turn {
  net: string;
  from: PlacedPin;
  to: PlacedPin[];
  /** Where it turns, once given a track. */
  x?: number;
}

/**
 * Draws each net's wire:
 * - a net joining three or more parts is a bus: from its driver's pin to
 *   a vertical in the gap, with a branch to each part it feeds;
 * - a net between two parts in neighbouring columns turns in the gap;
 * - a net between parts further apart goes along a channel below the
 *   columns in between;
 * - a stub leaves a box for a net on one part, staggered so a lower pin's
 *   stub is shorter and what hangs from an upper one drops past it.
 * In each gap every wire turning there gets its own track, ordered so
 * they don't cross: going down, a higher source turns later.
 */
export function route(d: Diagram, boxes: Box[]): Routed {
  const wires: Wire[] = [];
  const grounds: Pt[] = [];
  const dots: Pt[] = [];
  const trunks = new Map<string, Trunk[]>();
  const add = (t: Trunk) => trunks.set(t.net, [...(trunks.get(t.net) ?? []), t]);
  const byName = new Map(d.nets.map((n) => [n.name, n]));
  const boxOf = new Map(boxes.map((b) => [b.part.id, b]));
  const reachOf = new Map<string, number>(); // "<box>:<side>" → how far its stubs reach

  // Stubs leaving the board, staggered per side from the lowest pin up.
  for (const b of boxes) {
    for (const side of ['right', 'left'] as const) {
      const dir = side === 'right' ? 1 : -1;
      let far = side === 'right' && d.passives.some((q) => q.part === b.part.id && q.mount === 'bridge') ? 120 : 0;
      // A pin tied to another joins that pin's stub (below), not a stub of its own.
      const outs = b.pins.filter((p) => p.side === side && !p.tied && byName.get(p.net)?.wire === 'out').sort((p, q) => q.y - p.y);
      for (const p of outs) {
        const n = byName.get(p.net)!;
        const drops = d.passives.filter((q) => q.mount === 'out' && q.nets[0] === p.net).length;
        const len = far + 30 + drops * 80 + (n.arrow ? 40 : 30);
        const end = p.x + dir * len;
        const wire: Wire = { net: p.net, path: `M${p.x},${p.y} H${end}`, arrow: n.arrow, label: { x: end - dir * (n.arrow ? 12 : 4), y: p.y - 7, anchor: side === 'right' ? 'end' : 'start', text: p.net } };
        wires.push(wire);
        add({ net: p.net, x1: Math.min(p.x + dir * (far + 20), end), x2: Math.max(p.x + dir * (far + 20), end), y: p.y, stub: { wire, pin: p, dir } });
        far = len;
      }
      reachOf.set(`${b.part.id}:${side}`, far);
    }
  }

  // Gaps between columns: where wires turn.
  // A gap is named by the column on its left.
  const colRight = (c: number) => Math.max(PAD, ...boxes.filter((b) => b.part.column === c).map((b) => b.x + b.w + (reachOf.get(`${b.part.id}:right`) ?? 0)));
  const colLeft = (c: number) => Math.min(...boxes.filter((b) => b.part.column === c).map((b) => b.x - (reachOf.get(`${b.part.id}:left`) ?? 0)));
  const colOf = (id: string) => boxOf.get(id)!.part.column;
  const turns = new Map<number, Turn[]>(); // gap (the column on its left) → wires turning there
  const turnIn = (gap: number, t: Turn) => turns.set(gap, [...(turns.get(gap) ?? []), t]);
  const channels: { net: string; a: PlacedPin; z: PlacedPin; down: Turn; up: Turn }[] = [];
  for (const n of d.nets.filter((x) => x.wire === 'between' || x.wire === 'lane')) {
    const pins = n.parts.map((id) => ({ id, pin: pinOn(boxes, id, n.name) })).filter((e): e is { id: string; pin: PlacedPin } => !!e.pin);
    if (pins.length < 2) continue;
    // The driver: a pin facing right (what makes the net), else the leftmost.
    const sorted = [...pins].sort((a, b) => a.pin.x - b.pin.x);
    const driver = sorted.find((e) => e.pin.side === 'right') ?? sorted[0];
    const fed = pins.filter((e) => e !== driver);
    const dc = boxOf.get(driver.id)!.part.column;
    // Parts more than a column away are reached along a channel under the
    // columns in between (their own branch of the net).
    const far = fed.filter((e) => Math.abs(colOf(e.id) - dc) > 1);
    for (const e of far) {
      const [a, z, ia, iz] = driver.pin.x < e.pin.x ? [driver.pin, e.pin, driver.id, e.id] : [e.pin, driver.pin, e.id, driver.id];
      const down: Turn = { net: n.name, from: a, to: [a] };
      const up: Turn = { net: n.name, from: z, to: [z] };
      channels.push({ net: n.name, a, z, down, up });
      turnIn(colOf(ia), down);
      turnIn(colOf(iz) - 1, up);
    }
    const near = fed.filter((e) => !far.includes(e));
    const right = near.filter((e) => e.pin.x > driver.pin.x);
    const left = near.filter((e) => e.pin.x <= driver.pin.x);
    // Turning in the gap on the driver's side towards what it feeds.
    if (right.length) turnIn(dc, { net: n.name, from: driver.pin, to: right.map((e) => e.pin) });
    if (left.length) turnIn(dc - 1, { net: n.name, from: driver.pin, to: left.map((e) => e.pin) });
  }

  // Channels: over or under the columns in between, whichever passes
  // fewer wired pins at its two ends (each would be a crossing).
  // Only wires across the gap count: stubs end before the tracks, and
  // other channels nest.
  const channelNets = new Set(channels.map((c) => c.net));
  const acrossGap = (p: PlacedPin) => {
    const w = byName.get(p.net)?.wire;
    return (w === 'between' || w === 'lane') && !channelNets.has(p.net);
  };
  const wiredPast = (pin: PlacedPin, above: boolean) =>
    boxes.flatMap((b) => b.pins).filter((p) => p !== pin && p.side === pin.side && p.x === pin.x && acrossGap(p) && (above ? p.y < pin.y : p.y > pin.y)).length;
  const bottomOf = (b: Box) => b.y + b.h + (b.pins.some((p) => p.side === 'bottom' && !p.ground) ? 120 : GROUND + 24);
  const over = channels.filter((ch) => wiredPast(ch.a, true) + wiredPast(ch.z, true) < wiredPast(ch.a, false) + wiredPast(ch.z, false));
  const under = channels.filter((ch) => !over.includes(ch));
  // Nested: under the columns, the lower source outermost (lowest); over
  // them, the higher source outermost (highest).
  under.sort((p, q) => q.a.y - p.a.y);
  under.forEach((ch, i) => {
    const between = boxes.filter((b) => b.x > ch.a.x && b.x + b.w < ch.z.x);
    const y = Math.max(ch.a.y, ch.z.y, ...between.map(bottomOf)) + 20 + (under.length - 1 - i) * TRACK;
    ch.down.to = [{ ...ch.a, y }];
    ch.up.to = [{ ...ch.z, y }];
  });
  over.sort((p, q) => p.a.y - q.a.y);
  over.forEach((ch, i) => {
    const between = boxes.filter((b) => b.x + b.w > ch.a.x && b.x < ch.z.x);
    const y = Math.min(ch.a.y, ch.z.y, ...between.map((b) => b.y - TITLE)) - 14 - (over.length - 1 - i) * TRACK;
    ch.down.to = [{ ...ch.a, y }];
    ch.up.to = [{ ...ch.z, y }];
  });

  // Tracks: per gap, ordered so turning wires don't cross.
  for (const [gap, list] of turns) {
    const lo = colRight(gap) + 16;
    const hi = colLeft(gap + 1) - 16;
    const span = (t: Turn) => [Math.min(t.from.y, ...t.to.map((p) => p.y)), Math.max(t.from.y, ...t.to.map((p) => p.y))];
    const goesDown = (t: Turn) => span(t)[1] > t.from.y;
    const down = list.filter(goesDown).sort((a, b) => a.from.y - b.from.y);
    const up = list.filter((t) => !goesDown(t)).sort((a, b) => b.from.y - a.from.y);
    // The first in each list turns last (furthest from where it comes
    // from). Tracks are near the side wires come from, leaving the rest of
    // the gap for what hangs from them; wires coming from the right (a
    // channel's far end, a bus fed leftwards) mirror.
    const fromLeft = (t: Turn) => t.from.x <= lo;
    const rightward = [...down, ...up].filter(fromLeft);
    const leftward = [...down, ...up].filter((t) => !fromLeft(t));
    rightward.forEach((t, k) => (t.x = Math.min(hi - 20, lo + 20 + (rightward.length - 1 - k) * TRACK)));
    leftward.forEach((t, k) => (t.x = Math.max(lo + 20, hi - 20 - (leftward.length - 1 - k) * TRACK)));
  }

  // Draw the turns: from a pin across to the track, along it, and to
  // each pin it feeds (a bus has a junction where a branch leaves).
  for (const list of turns.values())
    for (const t of list) {
      const x = t.x!;
      const ys = [t.from.y, ...t.to.map((p) => p.y)];
      const label = { x: (t.from.x + x) / 2, y: t.from.y - 7, anchor: 'middle' as const, text: t.net };
      const isChannelEnd = channels.some((c) => c.down === t || c.up === t);
      if (t.to.length === 1 && t.to[0].y === t.from.y && !isChannelEnd) {
        // Straight across: labelled in the middle.
        wires.push({ net: t.net, path: `M${t.from.x},${t.from.y} H${t.to[0].x}`, label: { ...label, x: (t.from.x + t.to[0].x) / 2 } });
        add({ net: t.net, x1: Math.min(t.from.x, t.to[0].x) + 20, x2: Math.max(t.from.x, t.to[0].x) - 20, y: t.from.y });
        continue;
      }
      wires.push({ net: t.net, path: `M${t.from.x},${t.from.y} H${x}`, label: isChannelEnd && t === channels.find((c) => c.up === t)?.up ? undefined : label });
      wires.push({ net: t.net, path: `M${x},${Math.min(...ys)} V${Math.max(...ys)}` });
      add({ net: t.net, x1: Math.min(t.from.x, x) + 20, x2: Math.max(t.from.x, x) - 14, y: t.from.y });
      for (const p of t.to) {
        if (isChannelEnd) continue;
        wires.push({ net: t.net, path: `M${x},${p.y} H${p.x}` });
        add({ net: t.net, x1: Math.min(x, p.x) + 14, x2: Math.max(x, p.x) - 20, y: p.y });
        if (p.y !== Math.min(...ys) && p.y !== Math.max(...ys)) dots.push({ x, y: p.y });
      }
      if (t.from.y !== Math.min(...ys) && t.from.y !== Math.max(...ys)) dots.push({ x, y: t.from.y });
    }
  // A channel's run under the columns between its two turns.
  for (const ch of channels) {
    const y = ch.down.to[0].y;
    wires.push({ net: ch.net, path: `M${ch.down.x},${y} H${ch.up.x}` });
    add({ net: ch.net, x1: ch.down.x! + 14, x2: ch.up.x! - 14, y });
  }

  // Tied pins: out, and along to the wire of the pin they're tied to.
  for (const b of boxes)
    for (const p of b.pins.filter((x) => x.tied && (x.side === 'left' || x.side === 'right'))) {
      const to = b.pins.find((x) => x.name === p.tied && !x.tied);
      if (!to) continue;
      const x = p.x + (p.side === 'right' ? 10 : -10);
      wires.push({ net: p.net, path: `M${p.x},${p.y} H${x} V${to.y}` });
      dots.push({ x, y: to.y });
    }

  // Ground pins: a short stub to a ground symbol each.
  for (const b of boxes)
    for (const p of b.pins.filter((x) => x.ground)) {
      wires.push({ net: p.net, path: `M${p.x},${p.y} V${p.y + 8}`, thin: true });
      grounds.push({ x: p.x, y: p.y + 8 });
    }
  return { wires, grounds, dots, trunks };
}

// ── hang ──────────────────────────────────────────────────────────────

/** A passive's size from its text: its name and value, no more. */
export function passiveSize(p: DiagramPassive): { w: number; h: number } {
  return { w: Math.max(52, 7.5 * Math.max(p.label.length, (p.value ?? '').length) + 18), h: p.value ? 40 : 26 };
}

/** What a placed passive must keep clear of. */
interface Clear {
  rects: Rect[];
  segs: Seg[];
}

/** A net's name at the end of a passive whose other end isn't ground (a pull-up's 3V3). */
export interface Flag extends Pt {
  text: string;
  /** Above the passive (a rail), else below it. */
  up: boolean;
}

/** How a passive's far end is shown: a ground symbol, or the net it goes to. */
type End = { ground: true } | { ground: false; text: string };

/** Where a passive's end symbol goes, for a passive at box hanging in dir. */
function endRect(end: End, box: Rect, up: boolean): Rect {
  const cx = box.x + box.w / 2;
  const w = end.ground ? 14 : end.text.length * 7 + 8;
  return { x: cx - w / 2, y: up ? box.y - 18 : box.y + box.h, w, h: end.ground ? GROUND : 18 };
}

/**
 * Whether a passive fits at box, hung from y on a wire by a vertical at
 * x: the drop crosses no other net's wire or a box, and the passive and
 * its end symbol overlap nothing and sit on no other net's wire.
 */
function fits(c: Clear, net: string, x: number, y: number, box: Rect, end: Rect): boolean {
  const near = box.y > y ? box.y : box.y + box.h;
  const [top, bottom] = [Math.min(y, near), Math.max(y, near)];
  for (const r of [box, end]) {
    if (c.rects.some((o) => overlaps(o, r, 6))) return false;
    if (c.segs.some((s) => s.net !== net && overlaps(r, { x: s.x1, y: s.y1, w: s.x2 - s.x1, h: s.y2 - s.y1 }, 4))) return false;
  }
  if (c.segs.some((s) => s.net !== net && s.y1 === s.y2 && s.y1 > top && s.y1 < bottom && x >= s.x1 - 2 && x <= s.x2 + 2)) return false;
  return !c.rects.some((r) => x > r.x && x < r.x + r.w && r.y < bottom && r.y + r.h > top);
}

export interface Hung {
  passives: PlacedPassive[];
  wires: Wire[];
  grounds: Pt[];
  flags: Flag[];
  dots: Pt[];
}

/**
 * Hangs each passive. A bridge sits between its two pins. The others hang
 * from their wire (or bottom pin) on a short vertical, at the first place
 * along the wire where it crosses no other net and covers nothing (a stub
 * is made longer if it has to be): down to a ground symbol, or, between
 * two nets (a pull-up), with the other net's name at its end, up towards
 * a rail when there's room. Saved positions win.
 */
export function hang(d: Diagram, boxes: Box[], r: Routed): Hung {
  const out: Hung = { passives: [], wires: [], grounds: [], flags: [], dots: [] };
  const clear: Clear = {
    rects: boxes.map((b) => ({ x: b.x - 6, y: b.y - TITLE, w: b.w + 12, h: b.h + TITLE })),
    segs: [...r.wires.flatMap((w) => pieces(w.net, w.path))],
  };
  for (const w of r.wires) if (w.label) clear.rects.push(labelRect(w.label));
  for (const g of r.grounds) clear.rects.push({ x: g.x - 7, y: g.y, w: 14, h: GROUND - 8 });
  const kinds = new Map(d.nets.map((n) => [n.name, n.kind]));
  const endOf = (q: DiagramPassive): End => (q.nets[1] ? { ground: false, text: q.nets[1] } : { ground: true });
  const take = (p: PlacedPassive, wires: Wire[], end?: End, up = false) => {
    out.passives.push(p);
    out.wires.push(...wires);
    clear.rects.push(p);
    for (const w of wires) clear.segs.push(...pieces(w.net, w.path));
    if (!end) return;
    const cx = p.x + p.w / 2;
    clear.rects.push(endRect(end, p, up));
    if (end.ground) {
      out.wires.push({ net: 'GND', path: `M${cx},${p.y + p.h} V${p.y + p.h + 6}`, thin: true });
      out.grounds.push({ x: cx, y: p.y + p.h + 6 });
    } else {
      const y = up ? p.y : p.y + p.h;
      out.wires.push({ net: end.text, path: `M${cx},${y} V${y + (up ? -6 : 6)}`, thin: true });
      out.flags.push({ x: cx, y: y + (up ? -6 : 6), text: end.text, up });
    }
  };
  const net = (q: DiagramPassive) => q.nets[0];
  // Bridges first: their place is fixed, between their pins.
  for (const q of d.passives.filter((x) => x.mount === 'bridge')) {
    const [a, z] = q.nets
      .map((n) => pinOn(boxes, q.part, n))
      .filter((p): p is PlacedPin => !!p)
      .sort((m, k) => m.y - k.y);
    if (!a || !z) continue;
    const x = q.x ?? a.x + 40;
    const p = { passive: q, x, y: q.y ?? a.y - 15, w: 58, h: Math.max(44, z.y - a.y + 30) };
    take(p, [a, z].map((pin) => ({ net: pin.net, path: `M${pin.x},${pin.y} H${x}`, thin: true })));
  }
  // Then below bottom pins, then on stubs and wires.
  const rest = [...d.passives.filter((x) => x.mount === 'below'), ...d.passives.filter((x) => x.mount !== 'bridge' && x.mount !== 'below')];
  for (const q of rest) {
    const { w, h } = passiveSize(q);
    const end = endOf(q);
    if (q.x !== undefined && q.y !== undefined) {
      // Placed by hand: from the nearest point of its wire.
      const t = (r.trunks.get(net(q)) ?? [])[0];
      const pin = pinOn(boxes, q.part, net(q));
      const from = t ? { x: Math.min(Math.max(q.x + w / 2, t.x1), t.x2), y: t.y } : (pin ?? { x: q.x + w / 2, y: q.y - DROP });
      const up = q.y + h < from.y;
      const near = up ? q.y + h : q.y;
      take({ passive: q, x: q.x, y: q.y, w, h }, [{ net: net(q), path: `M${from.x},${from.y} V${(from.y + near) / 2} H${q.x + w / 2} V${near}`, thin: true }], end, up);
      continue;
    }
    if (q.mount === 'below') {
      const pin = pinOn(boxes, q.part, net(q));
      if (!pin) continue;
      let placed = false;
      for (let k = 0; k < 40 && !placed; k++) {
        const cx = pin.x + (k % 2 ? 1 : -1) * Math.ceil(k / 2) * 34;
        for (let dy = 0; dy < 200 && !placed; dy += 20) {
          const box = { x: cx - w / 2, y: pin.y + DROP - 10 + dy, w, h };
          const jog = pin.y + 14;
          if (fits(clear, net(q), cx, jog, box, endRect(end, box, false))) {
            const path = cx === pin.x ? `M${pin.x},${pin.y} V${box.y}` : `M${pin.x},${pin.y} V${jog} H${cx} V${box.y}`;
            take({ passive: q, ...box }, [{ net: net(q), path, thin: true }], end);
            placed = true;
          }
        }
      }
      continue;
    }
    // On a wire: the free spot along it nearest the pin of the part it
    // hangs by (the part it serves), or along a longer stub. A pull-up to
    // a rail hangs up first; the rest down first.
    const dirs = !end.ground && kinds.get(end.text) === 'power' ? [true, false] : [false, true];
    const trunks = r.trunks.get(net(q)) ?? [];
    const anchor = pinOn(boxes, q.part, net(q));
    let placed = false;
    for (let grow = 0; grow < 8 && !placed; grow++) {
      const spots = trunks.flatMap((t) => {
        const xs: { t: Trunk; cx: number }[] = [];
        for (let cx = t.x1 + w / 2 + 8; cx <= t.x2 - w / 2 - 8; cx += 6) xs.push({ t, cx });
        return xs;
      });
      if (anchor) spots.sort((a, b) => Math.abs(a.cx - anchor.x) + Math.abs(a.t.y - anchor.y) - (Math.abs(b.cx - anchor.x) + Math.abs(b.t.y - anchor.y)));
      for (const up of end.ground ? [false] : dirs) {
        for (const { t, cx } of spots) {
          const box = { x: cx - w / 2, y: up ? t.y - DROP - h : t.y + DROP, w, h };
          if (fits(clear, net(q), cx, t.y, box, endRect(end, box, up))) {
            take({ passive: q, ...box }, [{ net: net(q), path: `M${cx},${t.y} V${up ? box.y + h : box.y}`, thin: true }], end, up);
            out.dots.push({ x: cx, y: t.y });
            placed = true;
            break;
          }
        }
        if (placed) break;
      }
      // No room: make the stub longer, if it is one.
      const stub = trunks.find((t) => t.stub);
      if (!placed && stub?.stub) {
        stub.x1 += stub.stub.dir > 0 ? 0 : -60;
        stub.x2 += stub.stub.dir > 0 ? 60 : 0;
        const { wire, pin, dir } = stub.stub;
        const tip = dir > 0 ? stub.x2 + 30 : stub.x1 - 30;
        wire.path = `M${pin.x},${pin.y} H${tip}`;
        if (wire.label) wire.label.x = tip - dir * (wire.arrow ? 12 : 4);
        clear.segs = clear.segs.filter((s) => !(s.net === wire.net && s.y1 === pin.y && s.y2 === pin.y)).concat(pieces(wire.net, wire.path));
      } else if (!placed) break;
    }
    // Still nowhere: below everything, so it's drawn all the same.
    if (!placed) {
      const t = trunks[0];
      const pin = pinOn(boxes, q.part, net(q));
      const from = t ? { x: (t.x1 + t.x2) / 2, y: t.y } : (pin ?? { x: PAD, y: PAD });
      const y = Math.max(...clear.rects.map((c) => c.y + c.h)) + 24;
      take({ passive: q, x: from.x - w / 2, y, w, h }, [{ net: net(q), path: `M${from.x},${from.y} V${y}`, thin: true }], end);
      if (t) out.dots.push(from);
    }
  }
  return out;
}

// ── helpers ───────────────────────────────────────────────────────────

/** Where a wire's label is drawn, roughly. */
export function labelRect(l: NonNullable<Wire['label']>): Rect {
  const w = l.text.length * 7.5;
  const x = l.anchor === 'middle' ? l.x - w / 2 : l.anchor === 'end' ? l.x - w : l.x;
  return { x, y: l.y - 12, w, h: 15 };
}

/** The far corner of a wire, for the drawing's size. */
export function wireExtent(w: Wire): Pt {
  const xs = [...w.path.matchAll(/(?:M(-?[\d.]+),|H(-?[\d.]+))/g)].map((m) => Number(m[1] ?? m[2]));
  const ys = [...w.path.matchAll(/(?:,(-?[\d.]+)|V(-?[\d.]+))/g)].map((m) => Number(m[1] ?? m[2]));
  return { x: Math.max(0, ...xs) + (w.label?.anchor === 'start' ? 60 : 0), y: Math.max(0, ...ys) };
}
