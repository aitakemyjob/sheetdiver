// Places a project's block diagram. The backend decides what is drawn
// (core.Diagram: each box's pins and their sides, how each net's wire
// runs, where each passive hangs); this decides only where things go, in
// stages that each do one thing:
//
//   measure  → each box's size from its pins
//   place    → boxes in columns, stacked, saved positions, no overlaps
//   pinsAt   → where each pin is on its box
//   route    → each net's wire
//   mount    → each passive near what it hangs from
//   unclutter→ passives moved clear of everything, and their wires
import type { Diagram, DiagramPart, DiagramPassive, DiagramPin } from './types';

export const PITCH = 30; // between pins
export const TOP = 30; // first pin below a box's top
export const BOX_W = 200;
export const STUB = 46; // wire down to a hanging passive
export const PAD = 40;
const TITLE = 30; // a box's title above it
const LANE = 16; // between lanes

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
  /** Empty pin slots on the right between outputs and a bridged pair. */
  gap: number;
}

/** A wire (an SVG path) with an optional net label. */
export interface Wire {
  net: string;
  path: string;
  label?: { x: number; y: number; anchor: 'start' | 'middle' | 'end'; text: string };
  arrow?: boolean;
  /** A passive's own short wire. */
  thin?: boolean;
}

/** A passive with its place. */
export interface PlacedPassive extends Rect {
  passive: DiagramPassive;
  /** Name and value on one line (beside a connector's pin). */
  oneLine?: boolean;
}

export interface BlockDrawing {
  boxes: Box[];
  passives: PlacedPassive[];
  wires: Wire[];
  width: number;
  height: number;
  /** Some passives return to ground (said in a note under the drawing). */
  grounded: boolean;
}

/** Lays out a block diagram. */
export function layoutBlockDiagram(d: Diagram): BlockDrawing {
  const boxes = measure(d);
  const lanes = d.nets.filter((n) => n.wire === 'lane').map((n) => n.name);
  place(boxes, d, lanes.length);
  boxes.forEach(pinsAt);
  const { wires, anchors } = route(d, boxes, lanes);
  const mounted = mount(d, boxes, anchors);
  wires.push(...unclutter(mounted, boxes, wires));
  const placed = mounted.map((m) => m.placed);
  const extents = [...boxes.map((b) => ({ x: b.x + b.w, y: b.y + b.h })), ...placed.map((p) => ({ x: p.x + p.w, y: p.y + p.h })), ...wires.map(wireExtent)];
  return {
    boxes,
    passives: placed,
    wires,
    width: Math.max(320, ...extents.map((e) => e.x)) + PAD,
    height: Math.max(160, ...extents.map((e) => e.y)) + PAD,
    grounded: placed.some((p) => p.passive.mount !== 'bridge'),
  };
}

// ── measure ───────────────────────────────────────────────────────────

/** Sizes each box: tall enough for its pins, wide enough for its bottom pins. */
export function measure(d: Diagram): Box[] {
  return d.parts.map((part) => {
    const pins = part.pins.map((p) => ({ ...p, x: 0, y: 0 }));
    const left = pins.filter((p) => p.side === 'left' || p.side === 'inside').length;
    const right = pins.filter((p) => p.side === 'right');
    const bottom = pins.filter((p) => p.side === 'bottom').length;
    // Outputs with passives hanging below them need room before a
    // bridged pair (the inductor) under them.
    const hangs = d.passives.some((q) => q.part === part.id && q.mount === 'out' && right.some((p) => !p.bridged && p.net === q.nets[0]));
    const gap = hangs && right.some((p) => p.bridged) ? 3 : 0;
    const w = Math.max(BOX_W, (bottom + (part.ground.length ? 1 : 0)) * 84 + 24);
    const h = Math.max(120, TOP + Math.max(left, right.length + gap) * PITCH + (bottom || part.ground.length ? 44 : 10));
    return { part, pins, gap, x: 0, y: 0, w, h };
  });
}

// ── place ─────────────────────────────────────────────────────────────

/**
 * Places boxes in their columns, stacked within a column. A gap between
 * columns is as wide as the passives hanging in it need; lanes go above.
 * Saved positions win; overlapping boxes are then moved apart.
 */
export function place(boxes: Box[], d: Diagram, lanes: number): void {
  const cols: Box[][] = [];
  for (const b of boxes) (cols[b.part.column] ??= []).push(b);
  const top = PAD + TITLE + 10 + (lanes ? lanes * LANE + 10 : 0);
  let x = PAD + 70; // room for an input wire on the left
  for (const col of cols) {
    const list = col ?? [];
    const hanging = Math.max(0, ...list.map((b) => d.passives.filter((q) => q.part === b.part.id && (q.mount === 'out' || q.mount === 'wire')).length));
    let y = top;
    for (const b of list) {
      b.x = b.part.x ?? x;
      b.y = b.part.y ?? y;
      y += b.h + 70 + (b.pins.some((p) => p.side === 'bottom') ? 120 : 0);
    }
    x += Math.max(BOX_W, ...list.map((b) => b.w)) + Math.max(170, hanging * 76 + 70);
  }
  separate(boxes);
}

/** Whether two rectangles overlap, with a margin. */
export function overlaps(a: Rect, b: Rect, margin = 0): boolean {
  return a.x < b.x + b.w + margin && b.x < a.x + a.w + margin && a.y < b.y + b.h + margin && b.y < a.y + a.h + margin;
}

/** A box with its title. */
const withTitle = (b: Rect): Rect => ({ x: b.x, y: b.y - TITLE, w: b.w, h: b.h + TITLE });

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
        if (overlaps(withTitle(order[i]), withTitle(order[j]), 24)) {
          order[j].y = order[i].y + order[i].h + 70;
          moved = true;
        }
      }
    if (!moved) return;
  }
}

// ── pinsAt ────────────────────────────────────────────────────────────

/** Puts each pin on its side: left (then inside ones below), right (bridged last, after the gap), bottom. */
export function pinsAt(b: Box): void {
  [...b.pins.filter((p) => p.side === 'left'), ...b.pins.filter((p) => p.side === 'inside')].forEach((p, i) => Object.assign(p, { x: b.x, y: b.y + TOP + i * PITCH }));
  const right = b.pins.filter((p) => p.side === 'right');
  right.forEach((p, i) => Object.assign(p, { x: b.x + b.w, y: b.y + TOP + (i + (p.bridged ? b.gap : 0)) * PITCH }));
  b.pins.filter((p) => p.side === 'bottom').forEach((p, i) => Object.assign(p, { x: b.x + 50 + i * 84, y: b.y + b.h }));
}

// ── route ─────────────────────────────────────────────────────────────

/** Where a net's wire lets passives hang: between two pins, along a wire leaving one, or at a pin. */
export type Anchor = { between: [PlacedPin, PlacedPin] } | { out: PlacedPin; end: number };

/** The pin of a box on a net. */
export function pinOn(boxes: Box[], part: string, net: string): PlacedPin | undefined {
  return boxes.find((b) => b.part.id === part)?.pins.find((p) => p.side !== 'inside' && p.net === net);
}

/** Draws each net's wire as the backend says it runs. */
export function route(d: Diagram, boxes: Box[], lanes: string[]): { wires: Wire[]; anchors: Map<string, Anchor> } {
  const wires: Wire[] = [];
  const anchors = new Map<string, Anchor>();
  for (const n of d.nets) {
    const pins = n.parts.map((id) => pinOn(boxes, id, n.name)).filter((p): p is PlacedPin => !!p);
    if (n.wire === 'lane') {
      // Each pin steps out from its box and up to a lane above the parts.
      const ly = PAD + 14 + lanes.indexOf(n.name) * LANE;
      const xs = pins.map((p) => (p.side === 'right' ? p.x + 14 : p.side === 'left' ? p.x - 14 : p.x));
      pins.forEach((p, i) => wires.push({ net: n.name, path: p.side === 'bottom' ? `M${p.x},${p.y} V${p.y + 14} H${xs[i]} V${ly}` : `M${p.x},${p.y} H${xs[i]} V${ly}` }));
      wires.push({ net: n.name, path: `M${Math.min(...xs)},${ly} H${Math.max(...xs)}`, label: { x: Math.min(...xs) + 4, y: ly - 4, anchor: 'start', text: n.name } });
    } else if (n.wire === 'between' && pins.length === 2) {
      const [a, z] = [...pins].sort((p, q) => p.x - q.x);
      const mx = (a.x + z.x) / 2;
      wires.push({
        net: n.name,
        path: a.y === z.y ? `M${a.x},${a.y} H${z.x}` : `M${a.x},${a.y} H${mx} V${z.y} H${z.x}`,
        label: { x: mx, y: Math.min(a.y, z.y) - 10, anchor: 'middle', text: n.name },
      });
      anchors.set(n.name, { between: [a, z] });
    } else if (n.wire === 'out' && pins.length === 1) {
      const p = pins[0];
      const hanging = d.passives.filter((q) => q.mount === 'out' && q.nets[0] === n.name).length;
      const len = Math.max(110, hanging * 76 + 50);
      const end = p.side === 'right' ? p.x + len : p.x - len;
      wires.push({ net: n.name, path: `M${p.x},${p.y} H${end}`, arrow: n.arrow, label: { x: end, y: p.y - 10, anchor: p.side === 'right' ? 'end' : 'start', text: n.name } });
      anchors.set(n.name, { out: p, end });
    }
  }
  return { wires, anchors };
}

// ── mount ─────────────────────────────────────────────────────────────

/** A passive placed near what it hangs from, and where its wires attach. */
export interface Mounted {
  placed: PlacedPassive;
  attach: Pt[];
  /** down: a wire down from the attachment; side: across then down; right: across. */
  dir: 'down' | 'side' | 'right';
}

/** A passive's size from its text. */
function size(p: DiagramPassive): { w: number; h: number } {
  return { w: Math.max(56, 8 * Math.max(p.label.length, (p.value ?? '').length) + 16), h: p.value ? 42 : 26 };
}

/** Places each passive where the backend says it hangs. */
export function mount(d: Diagram, boxes: Box[], anchors: Map<string, Anchor>): Mounted[] {
  const out: Mounted[] = [];
  const nth = (q: DiagramPassive) => {
    const alike = d.passives.filter((x) => x.mount === q.mount && x.part === q.part && x.nets[0] === q.nets[0]);
    return [alike.indexOf(q), alike.length] as const;
  };
  const beside = new Map<string, number>(); // connector pin → next free x
  for (const q of d.passives) {
    const { w, h } = size(q);
    const [i, n] = nth(q);
    const pin = pinOn(boxes, q.part, q.nets[0]);
    const anchor = anchors.get(q.nets[0]);
    const at = (x: number, y: number, attach: Pt[], dir: Mounted['dir'], extra: Partial<PlacedPassive> = {}) => out.push({ placed: { passive: q, x, y, w, h, ...extra }, attach, dir });
    switch (q.mount) {
      case 'wire': {
        if (!anchor || !('between' in anchor)) break;
        const [a, z] = anchor.between;
        const cx = a.x + ((z.x - a.x) * (i + 1)) / (n + 1);
        at(cx - w / 2, a.y + STUB + 10, [{ x: cx, y: a.y }], 'down');
        break;
      }
      case 'out': {
        if (!anchor || !('out' in anchor)) break;
        const p = anchor.out;
        const from = p.side === 'right' ? p.x : anchor.end + 30;
        const cx = from + ((Math.abs(anchor.end - p.x) - 30) * (i + 1)) / (n + 1);
        at(cx - w / 2, p.y + STUB, [{ x: cx, y: p.y }], 'down');
        break;
      }
      case 'lane':
        if (pin) at(pin.side === 'left' ? pin.x - 50 - i * 70 - w : pin.x + 50 + i * 70, pin.y + STUB, [pin], 'side');
        break;
      case 'below':
        if (pin) at(pin.x + i * 70 - w / 2, pin.y + STUB, [pin], 'down');
        break;
      case 'beside': {
        if (!pin) break;
        // One line, so neighbouring pins' passives fit side by side.
        const key = `${q.part}:${pin.net}`;
        const x = beside.get(key) ?? pin.x + 18;
        const ow = 7.5 * (q.label.length + (q.value ?? '').length + 1) + 16;
        out.push({ placed: { passive: q, x, y: pin.y - 12, w: ow, h: 24, oneLine: true }, attach: [pin], dir: 'right' });
        beside.set(key, x + ow + 10);
        break;
      }
      case 'bridge': {
        const ends = q.nets.map((net) => pinOn(boxes, q.part, net)).filter((p): p is PlacedPin => !!p);
        if (ends.length !== 2) break;
        const [a, z] = ends.sort((m, k) => m.y - k.y);
        out.push({ placed: { passive: q, x: a.x + 60, y: a.y - 15, w: 66, h: Math.max(48, z.y - a.y + 30) }, attach: [a, z], dir: 'right' });
        break;
      }
    }
  }
  return out;
}

// ── unclutter ─────────────────────────────────────────────────────────

/** Where a wire's label is drawn, roughly. */
export function labelRect(l: NonNullable<Wire['label']>): Rect {
  const w = l.text.length * 7.5;
  const x = l.anchor === 'middle' ? l.x - w / 2 : l.anchor === 'end' ? l.x - w : l.x;
  return { x, y: l.y - 12, w, h: 15 };
}

/**
 * Moves each passive down (or right, for one beside or across pins)
 * past whatever it would cover (boxes with their titles, wire labels,
 * passives already placed), then draws its wires to where it ended up.
 */
export function unclutter(mounted: Mounted[], boxes: Box[], wires: Wire[]): Wire[] {
  const obstacles: Rect[] = boxes.map((b) => ({ x: b.x - 8, y: b.y - TITLE, w: b.w + 16, h: b.h + TITLE }));
  for (const w of wires) if (w.label) obstacles.push(labelRect(w.label));
  const out: Wire[] = [];
  for (const m of mounted) {
    const p = m.placed;
    for (let k = 0; k < 60; k++) {
      const hit = obstacles.find((o) => overlaps(o, p, 6));
      if (!hit) break;
      if (m.dir === 'right') p.x = hit.x + hit.w + 8;
      else p.y = hit.y + hit.h + 8;
    }
    obstacles.push(p);
    const net = p.passive.nets[0];
    for (const a of m.attach) {
      const cx = p.x + p.w / 2;
      if (m.dir === 'right') out.push({ net, path: `M${a.x},${a.y} H${p.x}`, thin: true });
      else if (m.dir === 'side') out.push({ net, path: `M${a.x},${a.y} H${cx} V${p.y}`, thin: true });
      else out.push({ net, path: a.x === cx ? `M${a.x},${a.y} V${p.y}` : `M${a.x},${a.y} V${a.y + 20} H${cx} V${p.y}`, thin: true });
    }
  }
  return out;
}

/** The far corner of a wire, for the drawing's size. */
export function wireExtent(w: Wire): Pt {
  const xs = [...w.path.matchAll(/(?:M(-?[\d.]+),|H(-?[\d.]+))/g)].map((m) => Number(m[1] ?? m[2]));
  const ys = [...w.path.matchAll(/(?:,(-?[\d.]+)|V(-?[\d.]+))/g)].map((m) => Number(m[1] ?? m[2]));
  return { x: Math.max(0, ...xs) + (w.label?.anchor === 'start' ? 60 : 0), y: Math.max(0, ...ys) };
}
