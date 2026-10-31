import type { EditDocument } from './documents';
import { behaviourOf, inward, type Element } from './elements';
import { boxOf, dist, overlaps, tidy } from './geometry';
import type { Box, Graphic, GraphicKind, Pad, Pt, Side, SymbolPin } from './model';
import { pick, type Hit } from './picking';

// Tools: what the pointer does. Each is a small state machine (its state
// a tagged value, every event a transition), made by a factory from its
// name. The editor hosts them; tools change the document only through
// the host, which keeps the undo history.

/** What a tool can see and do. */
export interface ToolHost {
  readonly doc: EditDocument;
  readonly selection: number[];
  readonly layer: string;
  /** The pick tolerance and grid, in mm at the current zoom. */
  readonly tol: number;
  readonly grid: number;
  /** A point snapped, not to the elements skipped. */
  snap(p: Pt, skip?: Set<number>): Pt;
  /** Changes the elements; record says to remember the state before for undo. */
  change(elements: Element[], selection: number[], record: boolean): void;
  select(selection: number[]): void;
  /** What's drawn while a tool works: an element to be, a selection box. */
  preview(elements: Element[], marquee?: Box | null): void;
}

/** A tool. */
export interface Tool {
  readonly name: ToolName;
  down(h: ToolHost, p: Pt, additive: boolean): void;
  move(h: ToolHost, p: Pt): void;
  up(h: ToolHost, p: Pt): void;
  /** A key; true when the tool used it. */
  key(h: ToolHost, key: string): boolean;
  /** A hint for the status line. */
  hint(): string;
}

/** The tools' names. */
export type ToolName = 'select' | GraphicKind | 'pad' | 'pin';

/** Stroke widths by layer: KiCad's defaults. */
export const widths: Record<string, number> = { silk: 0.12, fab: 0.1, courtyard: 0.05, copper: 0.2, body: 0.254 };

/** The text height new texts get on a document. */
const textSize = (doc: EditDocument) => (doc.kind === 'symbol' ? 1.27 : 1);

/** The box round some elements. */
export function boxOfElements(els: Element[]): Box | null {
  if (!els.length) return null;
  const bs = els.map((e) => behaviourOf(e).box(e));
  return { x0: Math.min(...bs.map((b) => b.x0)), y0: Math.min(...bs.map((b) => b.y0)), x1: Math.max(...bs.map((b) => b.x1)), y1: Math.max(...bs.map((b) => b.y1)) };
}

/** Edits of the selection any tool can make by key: delete, turn, nudge. */
function selectionKey(h: ToolHost, key: string): boolean {
  const els = h.doc.elements;
  const sel = h.selection.filter((i) => els[i]);
  if (!sel.length) return false;
  const step = h.grid > 0 ? h.grid : 0.1;
  const nudges: Record<string, [number, number]> = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] };
  if (key === 'Delete' || key === 'Backspace') {
    h.change(
      els.filter((_, i) => !sel.includes(i)),
      [],
      true,
    );
    return true;
  }
  if (key === 'r' || key === 'R') {
    const b = boxOfElements(sel.map((i) => els[i]))!;
    const o = { x: tidy((b.x0 + b.x1) / 2), y: tidy((b.y0 + b.y1) / 2) };
    h.change(
      els.map((e, i) => (sel.includes(i) ? behaviourOf(e).turn(e, o) : e)),
      sel,
      true,
    );
    return true;
  }
  if (nudges[key]) {
    const [dx, dy] = nudges[key];
    h.change(
      els.map((e, i) => (sel.includes(i) ? behaviourOf(e).move(e, dx, dy) : e)),
      sel,
      true,
    );
    return true;
  }
  if (key === 'Escape') {
    h.select([]);
    return true;
  }
  return false;
}

/** The select tool's states. */
type SelectState =
  | { s: 'idle' }
  | { s: 'pressed'; at: Pt; hit: Hit }
  | { s: 'moving'; from: Pt; base: Element[]; moved: boolean }
  | { s: 'reshaping'; index: number; handle: number; base: Element[]; moved: boolean }
  | { s: 'marquee'; from: Pt; to: Pt; additive: boolean };

/** Picks, moves and reshapes; drags a box to select. */
export class SelectTool implements Tool {
  readonly name = 'select' as const;
  private state: SelectState = { s: 'idle' };

  down(h: ToolHost, p: Pt, additive: boolean): void {
    const hit = pick(p, { elements: h.doc.elements, selection: h.selection, tol: h.tol });
    if (hit && hit.handle !== undefined) {
      this.state = { s: 'reshaping', index: hit.index, handle: hit.handle, base: h.doc.elements, moved: false };
      return;
    }
    if (hit) {
      const already = h.selection.includes(hit.index);
      if (additive) h.select(already ? h.selection.filter((i) => i !== hit.index) : [...h.selection, hit.index]);
      else if (!already) h.select([hit.index]);
      this.state = { s: 'pressed', at: p, hit };
      return;
    }
    if (!additive) h.select([]);
    this.state = { s: 'marquee', from: p, to: p, additive };
  }

  move(h: ToolHost, p: Pt): void {
    const st = this.state;
    switch (st.s) {
      case 'pressed':
        if (dist(p, st.at) > h.tol) this.state = { s: 'moving', from: st.at, base: h.doc.elements, moved: false };
        return this.move(h, p);
      case 'moving': {
        const sel = new Set(h.selection);
        // The move snaps the selection's first handle, so it lands on the grid or a point.
        const first = st.base[h.selection[0]];
        const anchor = first ? behaviourOf(first).handles(first)[0] : st.from;
        const to = h.snap({ x: anchor.x + p.x - st.from.x, y: anchor.y + p.y - st.from.y }, sel);
        const dx = to.x - anchor.x;
        const dy = to.y - anchor.y;
        h.change(
          st.base.map((e, i) => (sel.has(i) ? behaviourOf(e).move(e, dx, dy) : e)),
          h.selection,
          !st.moved,
        );
        st.moved = true;
        return;
      }
      case 'reshaping': {
        const q = h.snap(p, new Set([st.index]));
        h.change(
          st.base.map((e, i) => (i === st.index ? behaviourOf(e).reshape(e, st.handle, q) : e)),
          [st.index],
          !st.moved,
        );
        st.moved = true;
        return;
      }
      case 'marquee':
        st.to = p;
        h.preview([], boxOf([st.from, st.to]));
    }
  }

  up(h: ToolHost): void {
    const st = this.state;
    if (st.s === 'marquee') {
      const b = boxOf([st.from, st.to]);
      if (b.x1 - b.x0 > h.tol || b.y1 - b.y0 > h.tol) {
        const inBox = h.doc.elements.flatMap((e, i) => (overlaps(behaviourOf(e).box(e), b) ? [i] : []));
        h.select(st.additive ? [...new Set([...h.selection, ...inBox])] : inBox);
      }
      h.preview([], null);
    }
    this.state = { s: 'idle' };
  }

  key(h: ToolHost, key: string): boolean {
    return selectionKey(h, key);
  }

  hint(): string {
    return 'Click to select, shift-click to add, drag to move or box-select; drag a handle to reshape. R turns, Delete removes, arrows nudge.';
  }
}

/** How many clicks each drawn kind takes (0: until finished). */
const clicks: Record<GraphicKind, number> = { line: 2, rect: 2, circle: 2, arc: 3, poly: 0, text: 1 };

/** The graphic a drawing tool makes from the points clicked so far. */
function drawn(kind: GraphicKind, layer: string, pts: Pt[], doc: EditDocument): Graphic | null {
  const width = widths[layer] ?? 0.12;
  switch (kind) {
    case 'circle':
      return pts.length < 2 ? null : { kind, layer, pts: [pts[0]], r: tidy(Math.max(0.01, dist(pts[0], pts[1]))), width };
    case 'arc':
      return pts.length < 3 ? (pts.length === 2 ? { kind: 'line', layer, pts, width } : null) : { kind, layer, pts, width };
    case 'poly':
      return pts.length < 2 ? null : pts.length === 2 ? { kind: 'line', layer, pts, width } : { kind, layer, pts, width };
    case 'text':
      return { kind, layer, pts: [pts[0]], text: 'TEXT', size: textSize(doc), width: textSize(doc) * 0.15, angle: 0 };
  }
  return pts.length < 2 ? null : { kind, layer, pts, width };
}

/** A drawing tool's states. */
type DrawState = { s: 'idle' } | { s: 'collecting'; pts: Pt[] };

/** Draws a kind of graphic on the current layer, click by click. */
export class DrawTool implements Tool {
  private state: DrawState = { s: 'idle' };

  constructor(readonly name: GraphicKind) {}

  down(h: ToolHost, raw: Pt): void {
    const p = h.snap(raw);
    const pts = this.state.s === 'collecting' ? [...this.state.pts, p] : [p];
    const need = clicks[this.name];
    // A polygon closes on its first point.
    if (this.name === 'poly' && pts.length > 3 && dist(p, pts[0]) <= h.tol) return this.finish(h, pts.slice(0, -1));
    if (need && pts.length >= need) return this.finish(h, pts);
    this.state = { s: 'collecting', pts };
  }

  move(h: ToolHost, raw: Pt): void {
    if (this.state.s !== 'collecting') return;
    const g = drawn(this.name, h.layer, [...this.state.pts, h.snap(raw)], h.doc);
    h.preview(g ? [{ type: 'graphic', g }] : []);
  }

  up(): void {}

  key(h: ToolHost, key: string): boolean {
    if (this.state.s === 'collecting' && key === 'Enter' && this.name === 'poly') {
      this.finish(h, this.state.pts);
      return true;
    }
    if (this.state.s === 'collecting' && key === 'Escape') {
      this.state = { s: 'idle' };
      h.preview([]);
      return true;
    }
    return selectionKey(h, key);
  }

  /** Adds what was drawn, selected. */
  private finish(h: ToolHost, pts: Pt[]): void {
    this.state = { s: 'idle' };
    h.preview([]);
    const g = drawn(this.name, h.layer, pts, h.doc);
    if (!g || (g.kind === 'poly' && pts.length < 3)) return;
    const els = h.doc.elements;
    h.change([...els, { type: 'graphic', g }], [els.length], true);
  }

  hint(): string {
    const how: Record<GraphicKind, string> = {
      line: 'Click the start, then the end.',
      rect: 'Click one corner, then the opposite one.',
      circle: 'Click the centre, then a point on the circle.',
      arc: 'Click the start, a point on the arc, then the end.',
      poly: 'Click each corner; click the first again or press Enter to close it.',
      text: 'Click where the text starts, then type it in the properties.',
    };
    return how[this.name] + ' Escape cancels.';
  }
}

/** The next number after the highest numeric one used. */
function nextNumber(used: string[]): string {
  const ns = used.map((u) => parseInt(u, 10)).filter((n) => !isNaN(n));
  return String(ns.length ? Math.max(...ns) + 1 : 1);
}

/** Places pads, each like the last one placed, numbered on. */
export class PadTool implements Tool {
  readonly name = 'pad' as const;

  down(h: ToolHost, raw: Pt): void {
    const p = h.snap(raw);
    const els = h.doc.elements;
    const pads = els.flatMap((e) => (e.type === 'pad' ? [e.pad] : []));
    const last: Pad = pads[pads.length - 1] ?? { number: '1', kind: 'smd', shape: 'roundrect', x: 0, y: 0, w: 1, h: 0.6 };
    const pad: Pad = { ...last, number: nextNumber(pads.map((x) => x.number)), x: p.x, y: p.y, via: false };
    h.change([...els, { type: 'pad', pad }], [els.length], true);
  }

  move(): void {}
  up(): void {}

  key(h: ToolHost, key: string): boolean {
    return selectionKey(h, key);
  }

  hint(): string {
    return 'Click to place a pad like the last one, numbered on.';
  }
}

/** Places pins, pointing into the body from the side they're nearest. */
export class PinTool implements Tool {
  readonly name = 'pin' as const;

  down(h: ToolHost, raw: Pt): void {
    const p = h.snap(raw);
    const els = h.doc.elements;
    const pins = els.flatMap((e) => (e.type === 'pin' ? [e.pin] : []));
    const body = els.find((e) => e.type === 'body');
    let side: Side = 'left';
    if (body && body.type === 'body') {
      const b = body.box;
      const d: [Side, number][] = [
        ['left', Math.abs(p.x - b.x0)],
        ['right', Math.abs(p.x - b.x1)],
        ['top', Math.abs(p.y - b.y0)],
        ['bottom', Math.abs(p.y - b.y1)],
      ];
      side = d.sort((a, c) => a[1] - c[1])[0][0];
    }
    // The pin's end is outside the body: where it was clicked, one pin
    // length out from the body's side.
    const len = pins[pins.length - 1]?.length ?? 2.54;
    const dir = inward[side];
    const at = { x: tidy(p.x - dir.x * len), y: tidy(p.y - dir.y * len) };
    const pin: SymbolPin = { number: nextNumber(pins.map((x) => x.number)), name: '~', type: 'passive', side, x: at.x, y: at.y, length: len };
    h.change([...els, { type: 'pin', pin }], [els.length], true);
  }

  move(): void {}
  up(): void {}

  key(h: ToolHost, key: string): boolean {
    return selectionKey(h, key);
  }

  hint(): string {
    return 'Click on the body’s edge to add a pin there, pointing in; set its number, name and type in the properties.';
  }
}

/** The tools each kind of document has, in toolbar order. */
export const toolsFor: Record<EditDocument['kind'], ToolName[]> = {
  footprint: ['select', 'pad', 'line', 'rect', 'circle', 'arc', 'poly', 'text'],
  symbol: ['select', 'pin', 'line', 'rect', 'circle', 'arc', 'poly', 'text'],
};

/** Makes a tool by its name. */
export function createTool(name: ToolName): Tool {
  switch (name) {
    case 'select':
      return new SelectTool();
    case 'pad':
      return new PadTool();
    case 'pin':
      return new PinTool();
  }
  return new DrawTool(name);
}
