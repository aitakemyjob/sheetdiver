// A net picked on the drawn block diagram (its name clicked): what's on
// it, so the view can mark the net's wires and the parts it reaches.
// Nothing here draws.

import type { BlockDrawing, Pt } from '../blockDiagram';
import { pathPoints } from './lint';

/** What's on a picked net, by index or id into the drawing. */
export interface NetSelection {
  net: string;
  /** Wires of the net, by index. */
  wires: Set<number>;
  /** Junctions on the net's wires, by index. */
  dots: Set<number>;
  /** Net names at a passive's far end (a pull-up's 3V3), by index. */
  flags: Set<number>;
  /** Parts with a pin on the net, by part id. */
  parts: Set<string>;
  /** Passives with an end on the net, by passive id. */
  passives: Set<string>;
}

const same = (a: string | undefined, b: string) => !!a && a.toLowerCase() === b.toLowerCase();

/** Whether a point lies on a path of horizontal and vertical pieces. */
function onPath(p: Pt, path: string): boolean {
  const pts = pathPoints(path);
  return pts.slice(1).some((b, i) => {
    const a = pts[i];
    return p.x >= Math.min(a.x, b.x) && p.x <= Math.max(a.x, b.x) && p.y >= Math.min(a.y, b.y) && p.y <= Math.max(a.y, b.y);
  });
}

/** Everything on a net in a drawing. */
export function selectNet(d: BlockDrawing, net: string): NetSelection {
  const wires = new Set(d.wires.flatMap((w, i) => (same(w.net, net) ? [i] : [])));
  return {
    net,
    wires,
    dots: new Set(d.dots.flatMap((p, i) => ([...wires].some((w) => onPath(p, d.wires[w].path)) ? [i] : []))),
    flags: new Set(d.flags.flatMap((f, i) => (same(f.text, net) ? [i] : []))),
    parts: new Set(d.boxes.filter((b) => b.pins.some((p) => same(p.net, net))).map((b) => b.part.id)),
    passives: new Set(d.passives.filter((q) => q.passive.nets.some((n) => same(n, net))).map((q) => q.passive.id)),
  };
}

/** The net picked after a name is clicked: that net, or none when it was picked already. */
export function toggleNet(current: string | null, clicked: string): string | null {
  return current !== null && same(current, clicked) ? null : clicked;
}
