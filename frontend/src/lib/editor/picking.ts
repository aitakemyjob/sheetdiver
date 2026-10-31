import { behaviourOf, type Element } from './elements';
import { dist } from './geometry';
import type { Pt } from './model';

// What's under the pointer, and where a point snaps to: each a chain of
// rules tried in order, the first that answers winning. A new rule is a
// new link.

/** What the pointer is on: an element, maybe one of its handles. */
export interface Hit {
  index: number;
  handle?: number;
}

/** What picking knows. */
export interface PickContext {
  elements: Element[];
  selection: number[];
  tol: number;
}

/** A link of the picking chain: a hit, or null to pass it on. */
export type PickRule = (p: Pt, ctx: PickContext) => Hit | null;

/** A handle of something selected. */
export const pickHandle: PickRule = (p, ctx) => {
  for (const index of ctx.selection) {
    const e = ctx.elements[index];
    if (!e) continue;
    const hs = behaviourOf(e).handles(e);
    for (let h = 0; h < hs.length; h++) if (dist(p, hs[h]) <= ctx.tol * 1.5) return { index, handle: h };
  }
  return null;
};

/** Elements of some types, topmost first. */
export function pickTypes(types: Element['type'][]): PickRule {
  return (p, ctx) => {
    for (let i = ctx.elements.length - 1; i >= 0; i--) {
      const e = ctx.elements[i];
      if (types.includes(e.type) && behaviourOf(e).hit(e, p, ctx.tol)) return { index: i };
    }
    return null;
  };
}

/** The picking chain: handles, then pads and pins, then drawing, then the body. */
export const pickChain: PickRule[] = [pickHandle, pickTypes(['pad', 'pin']), pickTypes(['graphic']), pickTypes(['body'])];

/** What a point is on, by the chain. */
export function pick(p: Pt, ctx: PickContext, chain: PickRule[] = pickChain): Hit | null {
  for (const rule of chain) {
    const hit = rule(p, ctx);
    if (hit) return hit;
  }
  return null;
}

/** What snapping knows. */
export interface SnapContext {
  elements: Element[];
  /** Elements not to snap to (those being moved). */
  skip: Set<number>;
  grid: number;
  tol: number;
}

/** A link of the snapping chain: the snapped point, or null to pass it on. */
export type SnapRule = (p: Pt, ctx: SnapContext) => Pt | null;

/** Onto a nearby element's handle point (a line's end, a pad's corner). */
export const snapToPoints: SnapRule = (p, ctx) => {
  let best: Pt | null = null;
  let bestD = ctx.tol;
  ctx.elements.forEach((e, i) => {
    if (ctx.skip.has(i)) return;
    for (const h of behaviourOf(e).handles(e)) {
      const d = dist(p, h);
      if (d <= bestD) [best, bestD] = [h, d];
    }
  });
  return best;
};

/** Onto the grid. */
export const snapToGrid: SnapRule = (p, ctx) => {
  if (ctx.grid <= 0) return null;
  const r = (v: number) => Math.round((Math.round(v / ctx.grid) * ctx.grid) * 1e4) / 1e4;
  return { x: r(p.x), y: r(p.y) };
};

/** The snapping chain: points first, then the grid. */
export const snapChain: SnapRule[] = [snapToPoints, snapToGrid];

/** Where a point snaps to, by the chain (itself when nothing answers). */
export function snap(p: Pt, ctx: SnapContext, chain: SnapRule[] = snapChain): Pt {
  for (const rule of chain) {
    const q = rule(p, ctx);
    if (q) return q;
  }
  return p;
}
