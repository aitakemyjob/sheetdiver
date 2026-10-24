// A group's grid of parts: which column each node is in (nodes whose
// spans across the flow overlap share one) and its place in it from the
// top; and, the other way, the coordinates that ask ELK's interactive
// layering for a column and a place. Nothing here runs ELK.

/** A laid-out node. */
export interface Placed {
  id: string;
  x: number;
  y: number;
  w: number;
  h: number;
}

/** A place in the grid of parts, from 0 at the left and the top. */
export interface Cell {
  column: number;
  row: number;
}

/** A place asked for: a column, a row, or both. */
export interface Want {
  column?: number;
  row?: number;
}

/** The columns of a layout, left to right, each its nodes top to bottom. */
export function columns(nodes: Placed[]): Placed[][] {
  const sorted = [...nodes].sort((a, b) => a.x - b.x || a.y - b.y);
  const out: { right: number; nodes: Placed[] }[] = [];
  for (const n of sorted) {
    const last = out[out.length - 1];
    if (last && n.x < last.right) {
      last.nodes.push(n);
      last.right = Math.max(last.right, n.x + n.w);
    } else out.push({ right: n.x + n.w, nodes: [n] });
  }
  return out.map((c) => c.nodes.sort((a, b) => a.y - b.y));
}

/** Each node's cell in a layout. */
export function cells(nodes: Placed[]): Map<string, Cell> {
  const out = new Map<string, Cell>();
  columns(nodes).forEach((col, column) => col.forEach((n, row) => out.set(n.id, { column, row })));
  return out;
}

/**
 * Where to put each node for an interactive layout that gives the ones
 * asking for a place that place and keeps the rest where the first
 * layout put them: a node asking for a column centred on that column (or
 * past the last, a layer's step further for each); one asking for a row
 * just above the node that has that row now, or below the last. A node
 * with a dragged position keeps its column and goes where it was dragged
 * across the flow.
 */
export function hints(first: Placed[], wants: Map<string, Want>, dragged: Map<string, { y: number }> = new Map()): Map<string, { x: number; y: number }> {
  const cols = columns(first);
  const span = (col: Placed[]) => ({ left: Math.min(...col.map((n) => n.x)), right: Math.max(...col.map((n) => n.x + n.w)) });
  const step = cols.length > 1 ? span(cols[1]).left - span(cols[0]).left : 200;
  const centre = (c: number) => {
    if (c < cols.length) {
      const s = span(cols[c]);
      return (s.left + s.right) / 2;
    }
    const last = cols.length ? span(cols[cols.length - 1]) : { left: 0, right: 0 };
    return (last.left + last.right) / 2 + (c - cols.length + 1) * step;
  };
  const cell = cells(first);
  const out = new Map<string, { x: number; y: number }>();
  for (const n of first) out.set(n.id, { x: n.x, y: dragged.get(n.id)?.y ?? n.y });
  for (const n of first) {
    const want = wants.get(n.id);
    if (!want) continue;
    const column = want.column ?? cell.get(n.id)!.column;
    const x = want.column !== undefined ? centre(column) - n.w / 2 : n.x;
    let y = out.get(n.id)!.y;
    if (want.row !== undefined) {
      const others = (column < cols.length ? cols[column] : []).filter((o) => o.id !== n.id);
      y = want.row < others.length ? others[want.row].y - 1 : (others.length ? Math.max(...others.map((o) => o.y + o.h)) : 0) + 1;
    }
    out.set(n.id, { x, y });
  }
  return out;
}
