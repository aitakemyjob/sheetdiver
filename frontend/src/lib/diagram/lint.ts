// What looks wrong on a drawn block diagram, named so an agent can act on
// it without seeing the picture: a wire that leaves a pin and runs round
// its part, a wire through a part, parts on top of each other, wires of
// two nets crossing. Each check is a rule of its own over the drawing;
// they all run, and what they find is collected. Nothing here lays out.

import type { BlockDrawing, Box, Pt, Rect } from '../blockDiagram';

/** Something a check found, where it is. */
export interface Finding {
  text: string;
  at: Pt;
}

/** A finding with the group it's in. */
export interface LayoutProblem extends Finding {
  /** The check that found it. */
  check: string;
  group?: string;
}

/** A check over a drawing. */
export interface LayoutCheck {
  name: string;
  find(d: BlockDrawing): Finding[];
}

/** A straight piece of a wire, horizontal or vertical, its ends in order. */
interface Seg {
  net: string;
  x1: number;
  y1: number;
  x2: number;
  y2: number;
}

/** The corner points of an SVG path of absolute M, L, H and V commands. */
export function pathPoints(path: string): Pt[] {
  const out: Pt[] = [];
  let x = 0;
  let y = 0;
  for (const [, cmd, a, b] of path.matchAll(/([MLHV])(-?[\d.]+)(?:,(-?[\d.]+))?/g)) {
    if (cmd === 'M' || cmd === 'L') [x, y] = [Number(a), Number(b)];
    if (cmd === 'H') x = Number(a);
    if (cmd === 'V') y = Number(a);
    out.push({ x, y });
  }
  return out;
}

/** A drawing's wire pieces. */
function segments(d: BlockDrawing): Seg[] {
  return d.wires.flatMap((w) => {
    const pts = pathPoints(w.path);
    return pts.slice(1).map((p, i) => ({ net: w.net, x1: Math.min(p.x, pts[i].x), y1: Math.min(p.y, pts[i].y), x2: Math.max(p.x, pts[i].x), y2: Math.max(p.y, pts[i].y) }));
  });
}

const on = (p: Pt, s: Seg) => p.x >= s.x1 && p.x <= s.x2 && p.y >= s.y1 && p.y <= s.y2;
const ends = (s: Seg): Pt[] => [
  { x: s.x1, y: s.y1 },
  { x: s.x2, y: s.y2 },
];
const touch = (a: Seg, b: Seg) => ends(a).some((p) => on(p, b)) || ends(b).some((p) => on(p, a));
const inside = (p: Pt, r: Rect) => p.x >= r.x && p.x <= r.x + r.w && p.y >= r.y && p.y <= r.y + r.h;
/** Whether two rectangles share more than an edge. */
const overlap = (a: Rect, b: Rect) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;

/** The pieces of a net's wiring joined to a point, through its junctions. */
function wiredTo(p: Pt, segs: Seg[]): Seg[] {
  const out = segs.filter((s) => on(p, s));
  for (let i = 0; i < out.length; i++) for (const s of segs) if (!out.includes(s) && touch(out[i], s)) out.push(s);
  return out;
}

const OPPOSITE = { left: 'right', right: 'left', top: 'bottom', bottom: 'top' } as const;

/**
 * Whether a point is past a box's side opposite the given one: across the
 * flow anywhere (a wire from a left pin reaching the box's right went
 * over or under it); down or up only behind the box (a wire from a top
 * pin dropping beside the box to a part lower down hasn't gone round it).
 */
function past(p: Pt, b: Rect, side: keyof typeof OPPOSITE): boolean {
  const behind = p.x > b.x && p.x < b.x + b.w;
  if (side === 'left') return p.x > b.x + b.w;
  if (side === 'right') return p.x < b.x;
  if (side === 'top') return behind && p.y > b.y + b.h;
  return behind && p.y < b.y;
}

/**
 * A wire that leaves a pin and reaches past the far side of the pin's
 * own part: it loops round the part (a strap off a pin on the MCU's left
 * wired to something on its right).
 */
export const wireRoundPart: LayoutCheck = {
  name: 'wire round a part',
  find(d) {
    const segs = segments(d);
    const out: Finding[] = [];
    for (const b of d.boxes) {
      const seen = new Set<string>();
      for (const p of b.pins) {
        if (!p.net || p.ground || p.tied || p.dim || seen.has(p.net.toLowerCase())) continue;
        const net = segs.filter((s) => s.net === p.net);
        const far = wiredTo(p, net)
          .flatMap(ends)
          .find((x) => past(x, b, p.side));
        if (!far) continue;
        seen.add(p.net.toLowerCase());
        out.push({ text: `${p.net}'s wire leaves ${b.part.title}'s ${p.name} on its ${p.side} and runs round ${b.part.title} to its ${OPPOSITE[p.side]}`, at: { x: p.x, y: p.y } });
      }
    }
    return out;
  },
};

/** A wire through a part's body. */
export const wireThroughPart: LayoutCheck = {
  name: 'wire through a part',
  find(d) {
    const out: Finding[] = [];
    const segs = segments(d);
    for (const b of d.boxes) {
      const body = { x: b.body.x + 1, y: b.body.y + 1, w: b.body.w - 2, h: b.body.h - 2 };
      const nets = new Set(segs.filter((s) => overlap({ x: s.x1, y: s.y1, w: s.x2 - s.x1 || 0.5, h: s.y2 - s.y1 || 0.5 }, body)).map((s) => s.net));
      for (const net of nets) out.push({ text: `${net}'s wire runs through ${b.part.title}`, at: { x: b.body.x, y: b.body.y } });
    }
    return out;
  },
};

/** Parts and passives drawn on top of each other. */
export const overlappingParts: LayoutCheck = {
  name: 'parts overlapping',
  find(d) {
    const things: (Rect & { what: string })[] = [
      ...d.boxes.map((b: Box) => ({ x: b.body.x, y: b.body.y, w: b.body.w, h: b.body.h, what: b.part.title })),
      ...d.passives.map((q) => ({ x: q.x, y: q.y, w: q.w, h: q.h, what: q.passive.label })),
    ];
    const out: Finding[] = [];
    things.forEach((a, i) =>
      things.slice(i + 1).forEach((b) => {
        if (overlap(a, b)) out.push({ text: `${a.what} and ${b.what} are drawn on top of each other`, at: { x: a.x, y: a.y } });
      }),
    );
    return out;
  },
};

/** Wires of two nets crossing, once for each pair of nets. */
export const crossingWires: LayoutCheck = {
  name: 'wires crossing',
  find(d) {
    const segs = segments(d);
    const out = new Map<string, Finding>();
    for (const v of segs.filter((s) => s.x1 === s.x2 && s.y1 !== s.y2))
      for (const h of segs.filter((s) => s.y1 === s.y2 && s.x1 !== s.x2)) {
        if (v.net === h.net || !(v.x1 > h.x1 && v.x1 < h.x2 && h.y1 > v.y1 && h.y1 < v.y2)) continue;
        const [a, b] = [v.net, h.net].sort();
        const key = `${a}\u0000${b}`;
        if (!out.has(key)) out.set(key, { text: `${a} and ${b} cross`, at: { x: v.x1, y: h.y1 } });
      }
    return [...out.values()];
  },
};

/** The checks, in the order their findings are listed. */
export const LAYOUT_CHECKS: readonly LayoutCheck[] = [wireRoundPart, wireThroughPart, overlappingParts, crossingWires];

/** Runs every check over a drawing, naming the group each finding is in. */
export function lint(d: BlockDrawing, checks: readonly LayoutCheck[] = LAYOUT_CHECKS): LayoutProblem[] {
  return checks.flatMap((c) =>
    c.find(d).map((f) => {
      const group = d.frames.find((fr) => inside(f.at, fr))?.group.name;
      return { ...f, check: c.name, ...(group ? { group } : {}) };
    }),
  );
}
