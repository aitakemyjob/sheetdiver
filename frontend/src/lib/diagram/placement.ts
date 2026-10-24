// Where each group's frame goes: in columns, left to right in the order
// given (power flow), a group that names another as the one it's drawn
// below stacked under it in that group's column. Nothing here knows the
// drawing: it places rectangles.

/** A frame to place: its group's name, the group it's drawn below, and its size. */
export interface Frame {
  name: string;
  below?: string;
  w: number;
  h: number;
}

/** A frame's place. */
export interface Placed {
  name: string;
  x: number;
  y: number;
  /** Its column, from 0. */
  column: number;
}

/**
 * Places frames in columns, `gap` apart. A frame below another goes
 * under it, after any already there, in its column; one below a frame
 * that isn't here, or in a loop of frames below each other, starts a
 * column of its own. A column is as wide as its widest frame; columns
 * follow the order of the first frame in each.
 */
export function placeFrames(frames: Frame[], gap: number): Placed[] {
  const key = (s: string) => s.toLowerCase();
  const byName = new Map(frames.map((f) => [key(f.name), f]));
  // The frame a frame hangs under, when that frame is here and following
  // the chain up never comes back to it (a frame below a loop it isn't in
  // still hangs under its frame, which starts a column).
  const parent = (f: Frame): Frame | undefined => {
    const p = f.below ? byName.get(key(f.below)) : undefined;
    if (!p) return undefined;
    const seen = new Set<string>();
    for (let at: Frame | undefined = p; at && !seen.has(key(at.name)); at = at.below ? byName.get(key(at.below)) : undefined) {
      if (key(at.name) === key(f.name)) return undefined;
      seen.add(key(at.name));
    }
    return p;
  };
  const children = new Map<string, Frame[]>();
  const roots: Frame[] = [];
  for (const f of frames) {
    const p = parent(f);
    if (p) children.set(key(p.name), [...(children.get(key(p.name)) ?? []), f]);
    else roots.push(f);
  }
  // Each column: its root, then what's below it, depth first in order.
  const columns = roots.map((root) => {
    const out: Frame[] = [];
    const walk = (f: Frame) => {
      out.push(f);
      for (const c of children.get(key(f.name)) ?? []) walk(c);
    };
    walk(root);
    return out;
  });
  const placed: Placed[] = [];
  let x = 0;
  columns.forEach((col, i) => {
    let y = 0;
    for (const f of col) {
      placed.push({ name: f.name, x, y, column: i });
      y += f.h + gap;
    }
    x += Math.max(...col.map((f) => f.w)) + gap;
  });
  // In the order given.
  const at = new Map(placed.map((p) => [key(p.name), p]));
  return frames.map((f) => at.get(key(f.name))!);
}
