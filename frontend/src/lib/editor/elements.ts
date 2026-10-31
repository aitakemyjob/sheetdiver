import { arcPoints, boxOf, circleThrough, dist, inside, segDist, shift, textBox, tidy } from './geometry';
import type { Box, Graphic, Pad, Pt, Side, SymbolPin } from './model';

// What an editor's drawing is made of: drawn graphics, pads (footprint),
// pins and the body box (symbol), all in view coordinates (mm, y down).
// How each kind is picked, moved and reshaped is its behaviour, found by
// a factory keyed on the element; a new kind is a new behaviour.

/** One thing in the drawing. */
export type Element = { type: 'graphic'; g: Graphic } | { type: 'pad'; pad: Pad } | { type: 'pin'; pin: SymbolPin } | { type: 'body'; box: Box };

/** How an element is picked, moved and reshaped. */
export interface Behaviour<E extends Element = Element> {
  /** Whether a point is on it, within a tolerance. */
  hit(e: E, p: Pt, tol: number): boolean;
  /** The points it can be reshaped by. */
  handles(e: E): Pt[];
  /** It moved. */
  move(e: E, dx: number, dy: number): E;
  /** It with a handle dragged to a point. */
  reshape(e: E, handle: number, p: Pt): E;
  /** The box round it. */
  box(e: E): Box;
  /** It turned a quarter counter-clockwise (as seen) about a point. */
  turn(e: E, o: Pt): E;
}

/** A point turned a quarter counter-clockwise as seen (y down) about o. */
function quarter(p: Pt, o: Pt): Pt {
  return { x: tidy(o.x + (p.y - o.y)), y: tidy(o.y - (p.x - o.x)) };
}

/** The four corners of a box. */
function corners(b: Box): Pt[] {
  return [
    { x: b.x0, y: b.y0 },
    { x: b.x1, y: b.y0 },
    { x: b.x1, y: b.y1 },
    { x: b.x0, y: b.y1 },
  ];
}

/** A box from two corners, any way round. */
function normal(a: Pt, b: Pt): Box {
  return { x0: Math.min(a.x, b.x), y0: Math.min(a.y, b.y), x1: Math.max(a.x, b.x), y1: Math.max(a.y, b.y) };
}

/** A graphic with its points replaced. */
function withPts(g: Graphic, pts: Pt[]): Graphic {
  return { ...g, pts };
}

/** How each kind of graphic is picked and reshaped. */
const graphicShapes: Record<Graphic['kind'], Pick<Behaviour<{ type: 'graphic'; g: Graphic }>, 'hit' | 'handles' | 'reshape'>> = {
  line: {
    hit: (e, p, tol) => segDist(p, e.g.pts[0], e.g.pts[1]) <= tol + (e.g.width ?? 0) / 2,
    handles: (e) => e.g.pts,
    reshape: (e, h, p) => ({ ...e, g: withPts(e.g, e.g.pts.map((q, i) => (i === h ? p : q))) }),
  },
  rect: {
    hit: (e, p, tol) => {
      const c = corners(normal(e.g.pts[0], e.g.pts[1]));
      return (e.g.fill && inside(p, c)) || c.some((a, i) => segDist(p, a, c[(i + 1) % 4]) <= tol);
    },
    handles: (e) => e.g.pts,
    reshape: (e, h, p) => ({ ...e, g: withPts(e.g, e.g.pts.map((q, i) => (i === h ? p : q))) }),
  },
  circle: {
    hit: (e, p, tol) => {
      const d = dist(p, e.g.pts[0]);
      return (e.g.fill && d <= (e.g.r ?? 0)) || Math.abs(d - (e.g.r ?? 0)) <= tol;
    },
    // The centre, and a point on its right to size it by.
    handles: (e) => [e.g.pts[0], { x: e.g.pts[0].x + (e.g.r ?? 0), y: e.g.pts[0].y }],
    reshape: (e, h, p) => (h === 0 ? { ...e, g: withPts(e.g, [p]) } : { ...e, g: { ...e.g, r: tidy(Math.max(0.01, dist(p, e.g.pts[0]))) } }),
  },
  arc: {
    hit: (e, p, tol) => {
      const pts = arcPoints(e.g.pts[0], e.g.pts[1], e.g.pts[2], 24);
      return pts.slice(1).some((b, i) => segDist(p, pts[i], b) <= tol);
    },
    handles: (e) => e.g.pts,
    reshape: (e, h, p) => {
      const pts = e.g.pts.map((q, i) => (i === h ? p : q));
      return circleThrough(pts[0], pts[1], pts[2]) ? { ...e, g: withPts(e.g, pts) } : e;
    },
  },
  poly: {
    hit: (e, p, tol) => (e.g.fill && inside(p, e.g.pts)) || e.g.pts.some((a, i) => segDist(p, a, e.g.pts[(i + 1) % e.g.pts.length]) <= tol),
    handles: (e) => e.g.pts,
    reshape: (e, h, p) => ({ ...e, g: withPts(e.g, e.g.pts.map((q, i) => (i === h ? p : q))) }),
  },
  text: {
    hit: (e, p, tol) => {
      const b = textBox(e.g);
      return p.x >= b.x0 - tol && p.x <= b.x1 + tol && p.y >= b.y0 - tol && p.y <= b.y1 + tol;
    },
    handles: (e) => [e.g.pts[0]],
    reshape: (e, _h, p) => ({ ...e, g: withPts(e.g, [p]) }),
  },
};

/** A drawn graphic. */
const graphic: Behaviour<{ type: 'graphic'; g: Graphic }> = {
  hit: (e, p, tol) => graphicShapes[e.g.kind].hit(e, p, tol),
  handles: (e) => graphicShapes[e.g.kind].handles(e),
  reshape: (e, h, p) => graphicShapes[e.g.kind].reshape(e, h, p),
  move: (e, dx, dy) => ({ ...e, g: withPts(e.g, e.g.pts.map((p) => shift(p, dx, dy))) }),
  box: (e) => {
    const g = e.g;
    switch (g.kind) {
      case 'circle':
        return { x0: g.pts[0].x - (g.r ?? 0), y0: g.pts[0].y - (g.r ?? 0), x1: g.pts[0].x + (g.r ?? 0), y1: g.pts[0].y + (g.r ?? 0) };
      case 'arc':
        return boxOf(arcPoints(g.pts[0], g.pts[1], g.pts[2], 24));
      case 'text':
        return textBox(g);
    }
    return boxOf(g.pts);
  },
  turn: (e, o) => {
    const g = { ...e.g, pts: e.g.pts.map((p) => quarter(p, o)) };
    if (g.kind === 'text') g.angle = ((g.angle ?? 0) + 90) % 360;
    return { ...e, g };
  },
};

/** A footprint pad: picked on its copper, sized from a corner about its centre. */
const pad: Behaviour<{ type: 'pad'; pad: Pad }> = {
  hit: (e, p, tol) => Math.abs(p.x - e.pad.x) <= e.pad.w / 2 + tol && Math.abs(p.y - e.pad.y) <= e.pad.h / 2 + tol,
  handles: (e) => [{ x: e.pad.x + e.pad.w / 2, y: e.pad.y + e.pad.h / 2 }],
  reshape: (e, _h, p) => ({ ...e, pad: { ...e.pad, w: tidy(Math.max(0.05, 2 * Math.abs(p.x - e.pad.x))), h: tidy(Math.max(0.05, 2 * Math.abs(p.y - e.pad.y))) } }),
  move: (e, dx, dy) => ({ ...e, pad: { ...e.pad, x: tidy(e.pad.x + dx), y: tidy(e.pad.y + dy) } }),
  box: (e) => ({ x0: e.pad.x - e.pad.w / 2, y0: e.pad.y - e.pad.h / 2, x1: e.pad.x + e.pad.w / 2, y1: e.pad.y + e.pad.h / 2 }),
  turn: (e, o) => {
    const c = quarter({ x: e.pad.x, y: e.pad.y }, o);
    return { ...e, pad: { ...e.pad, x: c.x, y: c.y, w: e.pad.h, h: e.pad.w } };
  },
};

/** Which way a pin runs from its end into the body, as seen (y down). */
export const inward: Record<Side, Pt> = { left: { x: 1, y: 0 }, right: { x: -1, y: 0 }, top: { x: 0, y: 1 }, bottom: { x: 0, y: -1 } };

/** The side a pin is on once turned a quarter counter-clockwise. */
const turned: Record<Side, Side> = { left: 'bottom', bottom: 'right', right: 'top', top: 'left' };

/** A pin's inner end. */
export function pinEnd(pin: SymbolPin): Pt {
  const d = inward[pin.side];
  return { x: pin.x + d.x * pin.length, y: pin.y + d.y * pin.length };
}

/** A symbol pin: picked along its line, moved by its end. */
const pin: Behaviour<{ type: 'pin'; pin: SymbolPin }> = {
  hit: (e, p, tol) => segDist(p, e.pin, pinEnd(e.pin)) <= tol,
  handles: (e) => [{ x: e.pin.x, y: e.pin.y }],
  reshape: (e, _h, p) => ({ ...e, pin: { ...e.pin, x: p.x, y: p.y } }),
  move: (e, dx, dy) => ({ ...e, pin: { ...e.pin, x: tidy(e.pin.x + dx), y: tidy(e.pin.y + dy) } }),
  box: (e) => boxOf([e.pin, pinEnd(e.pin)]),
  turn: (e, o) => {
    const c = quarter(e.pin, o);
    return { ...e, pin: { ...e.pin, x: c.x, y: c.y, side: turned[e.pin.side] } };
  },
};

/** A symbol's body box: picked on its outline, reshaped by its corners. */
const body: Behaviour<{ type: 'body'; box: Box }> = {
  hit: (e, p, tol) => {
    const c = corners(e.box);
    return c.some((a, i) => segDist(p, a, c[(i + 1) % 4]) <= tol);
  },
  handles: (e) => corners(e.box),
  reshape: (e, h, p) => {
    const opposite = corners(e.box)[(h + 2) % 4];
    return { ...e, box: normal(opposite, p) };
  },
  move: (e, dx, dy) => ({ ...e, box: { x0: tidy(e.box.x0 + dx), y0: tidy(e.box.y0 + dy), x1: tidy(e.box.x1 + dx), y1: tidy(e.box.y1 + dy) } }),
  box: (e) => e.box,
  turn: (e, o) => ({ ...e, box: normal(quarter({ x: e.box.x0, y: e.box.y0 }, o), quarter({ x: e.box.x1, y: e.box.y1 }, o)) }),
};

/** The behaviours by element type. */
const behaviours = { graphic, pad, pin, body } as unknown as Record<Element['type'], Behaviour>;

/** The behaviour of an element. */
export function behaviourOf(e: Element): Behaviour {
  return behaviours[e.type];
}
