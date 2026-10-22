// Puts a diagram's nodes in columns along the power flow: what makes a
// rail sits left of what it feeds; parts reached only by signals go right
// of the part they talk to.
import type { DiagramEdge, DiagramNode } from './types';

/**
 * The column of each node: the longest chain of power links leading to
 * it, then one right of the part it talks to for a node only reached by
 * signals. Nodes with no links go in a column of their own at the end.
 */
export function columns(nodes: DiagramNode[], edges: DiagramEdge[]): Map<string, number> {
  const col = new Map(nodes.map((n) => [n.id, 0]));
  const power = edges.filter((e) => e.kind === 'power');
  // Longest path; at most n rounds, so a loop in the power tree ends.
  for (let i = 0; i < nodes.length; i++) {
    let changed = false;
    for (const e of power) {
      const c = (col.get(e.from) ?? 0) + 1;
      if (c > (col.get(e.to) ?? 0) && c < nodes.length) {
        col.set(e.to, c);
        changed = true;
      }
    }
    if (!changed) break;
  }
  const feeds = new Set(power.map((e) => e.from));
  for (let i = 0; i < 3; i++) {
    for (const e of edges) {
      if (e.kind === 'signal' && !feeds.has(e.to) && (col.get(e.to) ?? 0) <= (col.get(e.from) ?? 0)) col.set(e.to, (col.get(e.from) ?? 0) + 1);
    }
  }
  const linked = new Set(edges.flatMap((e) => [e.from, e.to]));
  const last = Math.max(0, ...[...col.entries()].filter(([id]) => linked.has(id)).map(([, c]) => c));
  for (const n of nodes) if (!linked.has(n.id)) col.set(n.id, linked.size ? last + 1 : 0);
  return col;
}
