// Lays out a project's block diagram from its connection table: parts
// and connectors as boxes with their pins on the sides (inputs facing what
// feeds them, outputs facing what they feed, configuration pins along the
// bottom), wires between them labelled with their nets, and passives
// hanging off the net or pin they serve: to ground below a wire (a TVS,
// bulk capacitors), below a configuration pin (a resistor), or across two
// pins (an inductor). Pins tied to another pin of the same part are
// written inside the box ("EN→IN"); ground is a label, not a wire.
import { columns } from './blockLayout';
import type { Block, ConnectionRow, DiagramEdge, Net, ProjectDetail, ProjectPart } from './types';

/** How a part or block is drawn and coloured. */
export type Kind = 'ic' | 'connector' | 'module' | 'resistor' | 'capacitor' | 'inductor' | 'diode' | 'other';

const PITCH = 30; // between pins
const TOP = 30; // first pin below the box's top
const BOX_W = 200;
const STUB = 46; // wire to a hanging passive
const PAD = 40;

/** A pin drawn on a side of a box, or written inside it. */
export interface DrawPin {
  name: string;
  net: string;
  side: 'l' | 'r' | 'b' | 'in';
  /** Where its wire starts (the box edge), for l, r and b. */
  x: number;
  y: number;
  /** "EN→IN": the wired pin this one is tied to. */
  tied?: string;
}

/** A part or connector drawn as a box. */
export interface DrawBox {
  id: string;
  title: string;
  kind: Kind;
  x: number;
  y: number;
  w: number;
  h: number;
  pins: DrawPin[];
  /** Ground pins, written inside the box. */
  ground: string[];
}

/** A passive drawn as a small box with its name and value. */
export interface DrawPassive {
  key: string;
  label: string;
  value: string;
  kind: Kind;
  x: number;
  y: number;
  w: number;
  h: number;
  /** Name and value on one line (beside a connector's pin). */
  oneLine?: boolean;
}

/** A wire (an SVG path) with an optional net label. */
export interface DrawWire {
  net: string;
  path: string;
  label?: { x: number; y: number; anchor: 'start' | 'middle' | 'end'; text: string };
  arrow?: boolean;
  thin?: boolean;
}

export interface BlockDrawing {
  boxes: DrawBox[];
  passives: DrawPassive[];
  wires: DrawWire[];
  width: number;
  height: number;
  /** Some passives return to ground (said in a note under the drawing). */
  grounded: boolean;
}

/** An endpoint of the connection table: a BOM entry or a block. */
interface End {
  id: string;
  label: string;
  title: string;
  kind: Kind;
  value: string;
  part?: ProjectPart;
}

/** The kind of a part category or block kind. */
export function kindOf(category: string | undefined, block: boolean): Kind {
  switch (category) {
    case 'resistor':
    case 'capacitor':
    case 'inductor':
    case 'diode':
      return category;
    case 'tvs':
      return 'diode';
    case 'ferrite':
    case 'fuse':
      return 'other';
    case 'connector':
    case 'module':
      return category;
  }
  return block ? 'connector' : 'ic';
}

/** Whether a kind is drawn as a passive. */
export function twoTerminal(k: Kind): boolean {
  return k === 'resistor' || k === 'capacitor' || k === 'inductor' || k === 'diode' || k === 'other';
}

/** The endpoints of a project, keyed like the diagram's nodes. */
function endpoints(parts: ProjectPart[], blocks: Block[]): Map<string, End> {
  const out = new Map<string, End>();
  for (const p of parts) {
    const label = p.designator || p.mpn;
    // A passive's value is in its role when the BOM gave one ("10µF").
    const value = /^[\d.]+\s*[pnuµmkKMG]?(F|H|Ω|R|ohm)?\b/.test(p.role ?? '') ? (p.role ?? '') : '';
    out.set(`part:${p.id}`, { id: `part:${p.id}`, label, title: p.designator ? `${p.designator} ${p.mpn}` : p.mpn, kind: kindOf(p.category, false), value, part: p });
  }
  for (const b of blocks) {
    out.set(`block:${b.id}`, { id: `block:${b.id}`, label: b.name, title: b.name, kind: kindOf(b.kind, true), value: b.value ?? '' });
  }
  return out;
}

/** The node id of a connection's endpoint. */
const endOf = (r: ConnectionRow) => (r.entryId ? `part:${r.entryId}` : `block:${r.blockId}`);

/** Lays out the block diagram of a project. */
export function layoutBlockDiagram(d: ProjectDetail): BlockDrawing {
  const c = d.connections;
  const ends = endpoints(d.parts, c.blocks);
  const nets = new Map<string, Net>(c.nets.map((n) => [n.name.toLowerCase(), n]));
  const ground = (net: string) => nets.get(net.toLowerCase())?.kind === 'ground';
  const rows = c.connections.filter((r) => ends.has(endOf(r)));
  const onNet = new Map<string, ConnectionRow[]>();
  for (const r of rows) onNet.set(r.net.toLowerCase(), [...(onNet.get(r.net.toLowerCase()) ?? []), r]);
  const isPassive = (id: string) => twoTerminal(ends.get(id)!.kind);
  const mains = [...new Set(rows.map(endOf))].filter((id) => !isPassive(id));
  // Parts in the BOM with no connections are left out: the sketch is of
  // what's connected.
  const mainsOn = (net: string) => [...new Set((onNet.get(net.toLowerCase()) ?? []).map(endOf).filter((id) => !isPassive(id)))];

  // Columns: what feeds a net sits left of what it feeds.
  const links: DiagramEdge[] = [];
  for (const [net, rs] of onNet) {
    if (ground(net)) continue;
    const ids = mainsOn(net);
    for (let i = 0; i < ids.length; i++)
      for (let j = i + 1; j < ids.length; j++) {
        const [a, b] = feeds(ends.get(ids[j])!, ends.get(ids[i])!, rs[0].net) ? [ids[j], ids[i]] : [ids[i], ids[j]];
        links.push({ from: a, to: b, kind: 'power', label: net, nets: [net] });
      }
  }
  const col = columns(
    mains.map((id) => ({ id, label: id })),
    links,
  );

  // Passives: each hangs from its non-ground nets.
  type P = { id: string; nets: string[] };
  const passives: P[] = [...new Set(rows.map(endOf))]
    .filter(isPassive)
    .map((id) => ({ id, nets: [...new Set(rows.filter((r) => endOf(r) === id).map((r) => r.net))].filter((n) => !ground(n)) }));
  const passivesOn = (net: string) => passives.filter((p) => p.nets.some((n) => n.toLowerCase() === net.toLowerCase()));

  // Pins and their sides.
  const boxes = new Map<string, DrawBox>();
  const gaps = new Map<string, number>();
  for (const id of mains) {
    const e = ends.get(id)!;
    const box: DrawBox = { id, title: e.title, kind: e.kind, x: 0, y: 0, w: BOX_W, h: 0, pins: [], ground: [] };
    const wired = new Map<string, string>(); // net → the pin that carries it
    // On a net with several of its pins, the supply or input pin gets the
    // wire and the others are written as tied to it (EN→IN).
    const main = (x: ConnectionRow) => (/^(V?IN|VCC|VDD|VBUS|VBAT|V?OUT)$/i.test(x.pinName || x.pin || '') ? 0 : 1);
    const mine = rows.filter((x) => endOf(x) === id).sort((a, b) => main(a) - main(b));
    for (const r of mine) {
      const name = r.pinName || r.pin || r.net;
      if (ground(r.net)) {
        if (!box.ground.includes(name)) box.ground.push(name);
        continue;
      }
      const key = r.net.toLowerCase();
      if (wired.has(key)) {
        box.pins.push({ name, net: r.net, side: 'in', x: 0, y: 0, tied: wired.get(key) });
        continue;
      }
      wired.set(key, name);
      box.pins.push({ name, net: r.net, side: sideOf(id, e, r, mainsOn(r.net), col, passivesOn(r.net), rows), x: 0, y: 0 });
    }
    // A pin tied to ground is written as "SEL→GND".
    for (const r of rows.filter((x) => endOf(x) === id && ground(x.net))) {
      const name = r.pinName || r.pin;
      if (name && !/^(A|D|P)?GND|^VSS|^EP$|thermal/i.test(name)) {
        box.ground = box.ground.filter((g) => g !== name);
        box.pins.push({ name, net: r.net, side: 'in', x: 0, y: 0, tied: 'GND' });
      }
    }
    // Two pins bridged by a passive (LX1, LX2 and the inductor) sit
    // together on the right.
    for (const p of passives) {
      const mine = box.pins.filter((pin) => pin.side !== 'in' && p.nets.some((n) => n.toLowerCase() === pin.net.toLowerCase()));
      if (mine.length === 2) mine.forEach((pin) => (pin.side = 'r'));
    }
    const bridgedPin = (pin: DrawPin) => passives.some((q) => q.nets.length === 2 && q.nets.some((n) => n.toLowerCase() === pin.net.toLowerCase()) && box.pins.filter((bp) => q.nets.some((n) => n.toLowerCase() === bp.net.toLowerCase())).length === 2);
    box.pins.sort((a, b) => Number(a.side === 'r' && bridgedPin(a)) - Number(b.side === 'r' && bridgedPin(b)));
    const left = box.pins.filter((p) => p.side === 'l' || p.side === 'in');
    const right = box.pins.filter((p) => p.side === 'r');
    // Room under an output for what hangs from it, before a bridged pair.
    const gap = right.some(bridgedPin) && right.some((p) => !bridgedPin(p) && passivesOn(p.net).some((q) => q.nets.length <= 1)) ? 3 : 0;
    gaps.set(id, gap);
    const bottom = box.pins.filter((p) => p.side === 'b');
    box.w = Math.max(BOX_W, (bottom.length + (box.ground.length ? 1 : 0)) * 84 + 24);
    box.h = Math.max(120, TOP + Math.max(left.length, right.length + gap) * PITCH + (bottom.length || box.ground.length ? 44 : 10));
    boxes.set(id, box);
  }

  // Columns left to right, stacked within a column. A gap is as wide as
  // the passives hanging in it need.
  const byCol: string[][] = [];
  for (const id of mains) (byCol[col.get(id) ?? 0] ??= []).push(id);
  const colX: number[] = [];
  let x = PAD + 70; // room for an input wire on the left
  for (let i = 0; i < byCol.length; i++) {
    colX[i] = x;
    const ids = byCol[i] ?? [];
    const w = Math.max(BOX_W, ...ids.map((id) => boxes.get(id)!.w));
    // Passives on nets leaving this column to the right, and pairs beside it.
    const hanging = Math.max(
      0,
      ...ids.map((id) => {
        const outs = boxes.get(id)!.pins.filter((p) => p.side === 'r');
        return outs.reduce((n, p) => n + passivesOn(p.net).length, 0);
      }),
    );
    x += w + Math.max(170, hanging * 76 + 70);
  }
  const positions = new Map(c.diagram.nodes.filter((n) => n.x !== undefined && n.y !== undefined).map((n) => [n.id, { x: n.x!, y: n.y! }]));
  // Nets joining three or more parts run along lanes above the parts.
  const laneNets = [...onNet.keys()].filter((n) => !ground(n) && mainsOn(n).length >= 3);
  const laneY = new Map(laneNets.map((n, i) => [n, PAD + 14 + i * 16]));
  const top = PAD + 40 + (laneNets.length ? laneNets.length * 16 + 10 : 0);
  for (let i = 0; i < byCol.length; i++) {
    let y = top; // room for the titles (and lanes)
    for (const id of byCol[i] ?? []) {
      const b = boxes.get(id)!;
      const saved = positions.get(id);
      b.x = saved?.x ?? colX[i];
      b.y = saved?.y ?? y;
      y += b.h + 70 + (b.pins.some((p) => p.side === 'b') ? 120 : 0);
    }
  }
  separateBoxes([...boxes.values()]);

  // Pin positions.
  for (const b of boxes.values()) {
    const left = b.pins.filter((p) => p.side === 'l');
    const inside = b.pins.filter((p) => p.side === 'in');
    [...left, ...inside].forEach((p, i) => Object.assign(p, { x: b.x, y: b.y + TOP + i * PITCH }));
    const right = b.pins.filter((p) => p.side === 'r');
    const firstBridged = right.findIndex((p) => passives.some((q) => q.nets.length === 2 && q.nets.some((n) => n.toLowerCase() === p.net.toLowerCase())));
    right.forEach((p, i) => Object.assign(p, { x: b.x + b.w, y: b.y + TOP + (i + (firstBridged >= 0 && i >= firstBridged ? (gaps.get(b.id) ?? 0) : 0)) * PITCH }));
    b.pins.filter((p) => p.side === 'b').forEach((p, i) => Object.assign(p, { x: b.x + 50 + i * 84, y: b.y + b.h }));
  }

  // Wires, and passives with where their wires attach: below a point
  // (down) or beside one or two points (right).
  const wires: DrawWire[] = [];
  const placed: (DrawPassive & { attach: { x: number; y: number }[]; dir: 'down' | 'right' | 'side'; net: string })[] = [];
  const done = new Set<string>(); // passives placed
  const size = (g: { label: string; value: string }) => ({ w: Math.max(56, 8 * Math.max(g.label.length, g.value.length) + 16), h: g.value ? 42 : 26 });
  const hangBelow = (ps: P[], at: (i: number, n: number) => { x: number; y: number }, from: { x: number; y: number }, dir: 'down' | 'side' = 'down') => {
    const groups = group(ps.filter((p) => !done.has(p.id)), ends);
    groups.forEach((g, i) => {
      const pt = at(i, groups.length);
      const { w, h } = size(g);
      placed.push({ key: g.ids.join(','), label: g.label, value: g.value, kind: g.kind, x: pt.x - w / 2, y: pt.y, w, h, attach: [{ x: from.x < 0 ? pt.x : from.x, y: from.y }], dir, net: g.net });
      g.ids.forEach((id) => done.add(id));
    });
  };
  const pinsOn = (net: string) => [...boxes.values()].flatMap((b) => b.pins.filter((p) => p.side !== 'in' && p.net.toLowerCase() === net.toLowerCase()).map((p) => ({ b, p })));
  for (const key of new Set(rows.map((r) => r.net.toLowerCase()))) {
    if (ground(key)) continue;
    const ps = pinsOn(key);
    if (ps.length === 0) continue;
    const net = ps[0].p.net;
    const hang = passivesOn(net).filter((p) => p.nets.length <= 1);
    if (laneY.has(key)) {
      // A lane: each pin steps out from its box and up to the lane.
      const ly = laneY.get(key)!;
      const xs = ps.map(({ p }) => (p.side === 'r' ? p.x + 14 : p.side === 'l' ? p.x - 14 : p.x));
      for (const [i, { p }] of ps.entries()) wires.push({ net, path: p.side === 'b' ? `M${p.x},${p.y} V${p.y + 14} H${xs[i]} V${ly}` : `M${p.x},${p.y} H${xs[i]} V${ly}` });
      const x0 = Math.min(...xs);
      wires.push({ net, path: `M${x0},${ly} H${Math.max(...xs)}`, label: { x: x0 + 4, y: ly - 4, anchor: 'start', text: net } });
      // Its passives hang beside the pin that makes the rail (or the first).
      const src = ps.find(({ p }) => p.side === 'r') ?? ps[0];
      hangBelow(hang, (j) => ({ x: src.p.x + 50 + j * 70, y: src.p.y + STUB }), { x: src.p.x, y: src.p.y }, 'side');
      continue;
    }
    if (ps.length >= 2) {
      // Between boxes: right pin to left pin, left to right.
      const sorted = [...ps].sort((a, b) => a.p.x - b.p.x);
      for (let i = 0; i + 1 < sorted.length; i++) {
        const a = sorted[i].p;
        const z = sorted[i + 1].p;
        const mx = (a.x + z.x) / 2;
        wires.push({
          net,
          path: a.y === z.y ? `M${a.x},${a.y} H${z.x}` : `M${a.x},${a.y} H${mx} V${z.y} H${z.x}`,
          label: i === 0 ? { x: mx, y: Math.min(a.y, z.y) - 10, anchor: 'middle', text: net } : undefined,
        });
        if (i === 0) hangBelow(hang, (j, n) => ({ x: a.x + ((z.x - a.x) * (j + 1)) / (n + 1), y: a.y + STUB + 10 }), { x: -1, y: a.y });
      }
      continue;
    }
    const { b, p } = ps[0];
    if (p.side === 'b') {
      hangBelow(hang, (j) => ({ x: p.x + j * 70, y: p.y + STUB }), { x: p.x, y: p.y });
      continue;
    }
    // A connector's pin with only passives on it (CC1 and its Rd): the
    // passive sits beside it.
    if (b.kind === 'connector' && hang.length && nets.get(key)?.kind !== 'power') {
      let px = p.x + 18;
      for (const g of group(hang.filter((q) => !done.has(q.id)), ends)) {
        // One line, so neighbouring pins' passives fit side by side.
        const w = 7.5 * (g.label.length + g.value.length + 1) + 16;
        const h = 24;
        placed.push({ key: g.ids.join(','), label: g.label, value: g.value, kind: g.kind, x: px, y: p.y - h / 2, w, h, oneLine: true, attach: [{ x: p.x, y: p.y }], dir: 'right', net });
        g.ids.forEach((id) => done.add(id));
        px += w + 10;
      }
      continue;
    }
    // A pin whose net leaves the board (an output, an input), with any
    // passives hanging from it.
    const bridged = passives.some((q) => q.nets.length === 2 && q.nets.some((n) => n.toLowerCase() === key) && q.nets.every((n) => b.pins.some((bp) => bp.net.toLowerCase() === n.toLowerCase())));
    if (bridged) continue; // drawn with its pair below
    const len = Math.max(110, hang.length * 76 + 50);
    const right = p.side === 'r';
    const end = right ? p.x + len : p.x - len;
    const out = right && (ends.get(b.id)!.part?.outputRail?.toLowerCase() === key || nets.get(key)?.kind === 'power' || isOutput(p, rows, b.id));
    wires.push({ net, path: `M${p.x},${p.y} H${end}`, arrow: out, label: { x: end, y: p.y - 10, anchor: right ? 'end' : 'start', text: net } });
    hangBelow(hang, (j, n) => ({ x: (right ? p.x : end + 30) + ((len - 30) * (j + 1)) / (n + 1), y: p.y + STUB }), { x: -1, y: p.y });
  }
  // Passives across two pins of a box (the inductor on LX1 and LX2).
  for (const q of passives) {
    if (done.has(q.id) || q.nets.length !== 2) continue;
    const pins = q.nets.map((n) => pinsOn(n).find(({ p }) => p.side === 'r'));
    if (!pins[0] || !pins[1] || pins[0].b !== pins[1].b) continue;
    const [a, z] = pins.map((x) => x!.p).sort((m, n) => m.y - n.y);
    const g = group([q], ends)[0];
    placed.push({ key: q.id, label: g.label, value: g.value, kind: g.kind, x: a.x + 60, y: a.y - 15, w: 66, h: Math.max(48, z.y - a.y + 30), attach: [a, z], dir: 'right', net: a.net });
    done.add(q.id);
  }

  // Nothing overlaps: each passive moves down (or right) past whatever
  // it would cover, and its wire follows.
  const obstacles: Rect[] = [...boxes.values()].map((b) => ({ x: b.x - 8, y: b.y - 30, w: b.w + 16, h: b.h + 30 }));
  for (const w of wires) if (w.label) obstacles.push(labelRect(w.label));
  for (const p of placed) {
    for (let k = 0; k < 60; k++) {
      const hit = obstacles.find((o) => overlaps(o, p, 6));
      if (!hit) break;
      if (p.dir === 'right') p.x = hit.x + hit.w + 8;
      else p.y = hit.y + hit.h + 8;
    }
    obstacles.push(p);
    for (const a of p.attach) {
      if (p.dir === 'right') wires.push({ net: p.net, path: `M${a.x},${a.y} H${p.x}`, thin: true });
      else if (p.dir === 'side') wires.push({ net: p.net, path: `M${a.x},${a.y} H${p.x + p.w / 2} V${p.y}`, thin: true });
      else {
        const cx = p.x + p.w / 2;
        wires.push({ net: p.net, path: a.x === cx ? `M${a.x},${a.y} V${p.y}` : `M${a.x},${a.y} V${a.y + 20} H${cx} V${p.y}`, thin: true });
      }
    }
  }

  const all = [...boxes.values()];
  const extents = [...all.map((b) => ({ x: b.x + b.w, y: b.y + b.h })), ...placed.map((p) => ({ x: p.x + p.w, y: p.y + p.h })), ...wires.map(wireEnd)];
  return {
    boxes: all,
    passives: placed.map(({ attach: _a, dir: _d, net: _n, ...p }) => p),
    wires,
    width: Math.max(320, ...extents.map((e) => e.x)) + PAD,
    height: Math.max(160, ...extents.map((e) => e.y)) + PAD,
    grounded: passives.some((q) => q.nets.length <= 1 && done.has(q.id)),
  };
}

/** A rectangle. */
interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

/** Whether two rectangles overlap, with a margin. */
export function overlaps(a: Rect, b: Rect, margin = 0): boolean {
  return a.x < b.x + b.w + margin && b.x < a.x + a.w + margin && a.y < b.y + b.h + margin && b.y < a.y + a.h + margin;
}

/** Where a wire's label is drawn, roughly. */
function labelRect(l: NonNullable<DrawWire['label']>): Rect {
  const w = l.text.length * 7.5;
  const x = l.anchor === 'middle' ? l.x - w / 2 : l.anchor === 'end' ? l.x - w : l.x;
  return { x, y: l.y - 12, w, h: 15 };
}

/**
 * Moves boxes that overlap (with their titles above them) down until none
 * do: a part dragged onto another, or saved positions from an older
 * layout.
 */
export function separateBoxes(boxes: DrawBox[]): void {
  const order = [...boxes].sort((a, b) => a.y - b.y || a.x - b.x);
  for (let round = 0; round < order.length * order.length; round++) {
    let moved = false;
    for (let i = 0; i < order.length; i++)
      for (let j = i + 1; j < order.length; j++) {
        const a = order[i];
        const b = order[j];
        if (overlaps({ x: a.x, y: a.y - 30, w: a.w, h: a.h + 30 }, { x: b.x, y: b.y - 30, w: b.w, h: b.h + 30 }, 24)) {
          b.y = a.y + a.h + 70;
          moved = true;
        }
      }
    if (!moved) return;
  }
}

/** Whether a feeds b on net: a makes the rail b is powered from, or a is a connector. */
function feeds(a: End, b: End, net: string): boolean {
  const n = net.toLowerCase();
  if (a.part?.outputRail?.toLowerCase() === n && b.part?.outputRail?.toLowerCase() !== n) return true;
  if (b.part?.supplyRail?.toLowerCase() === n && a.part?.supplyRail?.toLowerCase() !== n) return true;
  return a.kind === 'connector' && b.kind !== 'connector' && !b.part?.outputRail;
}

/** Which side of its box a pin goes on. */
function sideOf(id: string, e: End, r: ConnectionRow, others: string[], col: Map<string, number>, hanging: { nets: string[] }[], rows: ConnectionRow[]): DrawPin['side'] {
  const net = r.net.toLowerCase();
  const rest = others.filter((o) => o !== id);
  if (rest.length) {
    const left = rest.some((o) => (col.get(o) ?? 0) < (col.get(id) ?? 0));
    const right = rest.some((o) => (col.get(o) ?? 0) > (col.get(id) ?? 0));
    // On a rail with parts on both sides, only what makes it faces right.
    if (left && right) return e.part?.outputRail?.toLowerCase() === net || e.kind === 'connector' ? 'r' : 'l';
    return right ? 'r' : 'l';
  }
  if (e.part?.supplyRail?.toLowerCase() === net) return 'l';
  if (e.part?.outputRail?.toLowerCase() === net || isOutput({ name: r.pinName ?? '' }, rows, id)) return 'r';
  // Only passives on it: a configuration pin, with its resistor below.
  if (hanging.length && hanging.every((p) => p.nets.length <= 1)) return e.kind === 'connector' ? 'r' : 'b';
  return e.kind === 'connector' ? 'r' : 'l';
}

/** Whether a pin drives its net (its type, or a name like VOUT). */
function isOutput(p: { name: string }, rows: ConnectionRow[], id: string): boolean {
  const r = rows.find((x) => endOf(x) === id && (x.pinName === p.name || x.pin === p.name));
  return /^(O|DO|AO)$/i.test(r?.pinType ?? '') || /^V?OUT/i.test(p.name);
}

/** Passives on the same net with the same value, as one ("C1,C2 10µF"). */
function group(ps: { id: string; nets: string[] }[], ends: Map<string, End>) {
  type G = { ids: string[]; label: string; value: string; kind: Kind; net: string };
  const byKey = new Map<string, G>();
  for (const p of ps) {
    const e = ends.get(p.id)!;
    const key = p.nets.length <= 1 ? `${p.nets[0] ?? ''}|${e.value}|${e.kind}` : p.id;
    const g = byKey.get(key);
    if (g) {
      g.ids.push(p.id);
      g.label += ',' + e.label;
    } else byKey.set(key, { ids: [p.id], label: e.label, value: e.value, kind: e.kind, net: p.nets[0] ?? '' });
  }
  return [...byKey.values()];
}

/** The far corner of a wire, for the drawing's size. */
function wireEnd(w: DrawWire): { x: number; y: number } {
  const xs = [...w.path.matchAll(/(?:M(-?[\d.]+),|H(-?[\d.]+))/g)].map((m) => Number(m[1] ?? m[2]));
  const ys = [...w.path.matchAll(/(?:,(-?[\d.]+)|V(-?[\d.]+))/g)].map((m) => Number(m[1] ?? m[2]));
  return { x: Math.max(0, ...xs) + (w.label?.anchor === 'start' ? 60 : 0), y: Math.max(0, ...ys) };
}
