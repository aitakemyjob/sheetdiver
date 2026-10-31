import type { Box, Graphic, Pt } from './model';

// Plane geometry for the editors: distances for picking, arcs, boxes.

/** The distance between two points. */
export function dist(a: Pt, b: Pt): number {
  return Math.hypot(a.x - b.x, a.y - b.y);
}

/** The distance from a point to a segment. */
export function segDist(p: Pt, a: Pt, b: Pt): number {
  const dx = b.x - a.x;
  const dy = b.y - a.y;
  const l2 = dx * dx + dy * dy;
  const t = l2 === 0 ? 0 : Math.max(0, Math.min(1, ((p.x - a.x) * dx + (p.y - a.y) * dy) / l2));
  return dist(p, { x: a.x + t * dx, y: a.y + t * dy });
}

/** Whether a point is inside a polygon. */
export function inside(p: Pt, pts: Pt[]): boolean {
  let hit = false;
  for (let i = 0, j = pts.length - 1; i < pts.length; j = i++) {
    const a = pts[i];
    const b = pts[j];
    if (a.y > p.y !== b.y > p.y && p.x < ((b.x - a.x) * (p.y - a.y)) / (b.y - a.y) + a.x) hit = !hit;
  }
  return hit;
}

/** The circle through three points, or null when they're in a line. */
export function circleThrough(a: Pt, b: Pt, c: Pt): { o: Pt; r: number } | null {
  const d = 2 * (a.x * (b.y - c.y) + b.x * (c.y - a.y) + c.x * (a.y - b.y));
  if (Math.abs(d) < 1e-9) return null;
  const a2 = a.x * a.x + a.y * a.y;
  const b2 = b.x * b.x + b.y * b.y;
  const c2 = c.x * c.x + c.y * c.y;
  const o = { x: (a2 * (b.y - c.y) + b2 * (c.y - a.y) + c2 * (a.y - b.y)) / d, y: (a2 * (c.x - b.x) + b2 * (a.x - c.x) + c2 * (b.x - a.x)) / d };
  return { o, r: dist(a, o) };
}

/** An arc as points along it, start to end through its middle point. */
export function arcPoints(a: Pt, m: Pt, b: Pt, steps = 32): Pt[] {
  const c = circleThrough(a, m, b);
  if (!c) return [a, b];
  const ang = (p: Pt) => Math.atan2(p.y - c.o.y, p.x - c.o.x);
  const t0 = ang(a);
  const up = (t: number) => (((t - t0) % (2 * Math.PI)) + 2 * Math.PI) % (2 * Math.PI);
  // Sweep the way that passes the middle point.
  const sweep = up(ang(m)) <= up(ang(b)) ? up(ang(b)) : up(ang(b)) - 2 * Math.PI;
  return Array.from({ length: steps + 1 }, (_, i) => {
    const t = t0 + (sweep * i) / steps;
    return { x: c.o.x + c.r * Math.cos(t), y: c.o.y + c.r * Math.sin(t) };
  });
}

/** A box round points. */
export function boxOf(pts: Pt[]): Box {
  const xs = pts.map((p) => p.x);
  const ys = pts.map((p) => p.y);
  return { x0: Math.min(...xs), y0: Math.min(...ys), x1: Math.max(...xs), y1: Math.max(...ys) };
}

/** Whether two boxes overlap. */
export function overlaps(a: Box, b: Box): boolean {
  return a.x0 <= b.x1 && b.x0 <= a.x1 && a.y0 <= b.y1 && b.y0 <= a.y1;
}

/** A text's rough box: its glyphs are about 0.6 of its height wide. */
export function textBox(g: Graphic): Box {
  const p = g.pts[0];
  const w = (g.text ?? '').length * (g.size ?? 1) * 0.6;
  const h = g.size ?? 1;
  const t = ((g.angle ?? 0) * Math.PI) / 180;
  // Along the text (cos, -sin) and up from it (-sin, -cos), y down.
  const along = { x: Math.cos(t), y: -Math.sin(t) };
  const upv = { x: -Math.sin(t), y: -Math.cos(t) };
  return boxOf([p, { x: p.x + along.x * w, y: p.y + along.y * w }, { x: p.x + upv.x * h, y: p.y + upv.y * h }, { x: p.x + along.x * w + upv.x * h, y: p.y + along.y * w + upv.y * h }]);
}

/** A number kept to four decimals, as the backend keeps them. */
export function tidy(v: number): number {
  return Math.round(v * 1e4) / 1e4;
}

/** A point snapped to a grid. */
export function onGrid(p: Pt, grid: number): Pt {
  if (grid <= 0) return { x: tidy(p.x), y: tidy(p.y) };
  return { x: tidy(Math.round(p.x / grid) * grid), y: tidy(Math.round(p.y / grid) * grid) };
}

/** A point moved. */
export function shift(p: Pt, dx: number, dy: number): Pt {
  return { x: tidy(p.x + dx), y: tidy(p.y + dy) };
}
