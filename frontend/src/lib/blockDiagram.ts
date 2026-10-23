// Places a project's block diagram. The backend decides what is drawn
// (core.Diagram: each box's pins and their sides, which group each box
// and passive is in, what each passive serves); this turns it into a
// graph for ELK (the Eclipse Layout Kernel, elkjs) and the result back
// into a drawing:
//
//   measure → each box's size from its pins, and where each pin is on it
//   graph   → boxes are nodes with their pins as fixed ports; passives and
//             the names of nets leaving the group are small nodes; a net
//             is an edge from the pin that drives it to each pin it feeds
//   ELK     → layered left to right in power-flow order, wires routed
//             orthogonally around everything, edges of a net merged into
//             a bus with junctions, labels placed where they fit
//   drawing → the boxes, passives and wires at ELK's positions, plus the
//             ground symbols and tie jogs ELK doesn't know about
//
// What it promises, for any board (tested on several): nothing overlaps,
// no wire touches or sits on another net's wire, none runs through a box
// or along its side, every label fits its wire, and wires seldom cross
// (ELK keeps crossings to a minimum, but can't always avoid one where a
// net's branches interleave with another's).
//
// A board in functional groups is laid out a group at a time (groups
// share no wires: a net between groups is a labelled stub in each), each
// framed with its name, description and notes, left to right in power
// flow order. A part dragged by hand is a hint to ELK: its layer and
// order follow where it was put, and the wires are routed again.
import type { ELK, ElkExtendedEdge, ElkNode, ElkPort } from 'elkjs/lib/elk-api';
import type { Diagram, DiagramGroup, DiagramPart, DiagramPassive, DiagramPin } from './types';

export const PITCH = 30; // between pins
export const TOP = 30; // first pin below a box's top
export const BOX_W = 200;
export const PAD = 40;
const TITLE = 30; // a box's title above it
const GROUND = 16; // a ground symbol's height, with its stub
const SLOT = 84; // between bottom pins
const CHAR = 7.5; // a label's width per character
/** The room a ground pin's name needs along the bottom. */
const groundSlot = (p: DiagramPin) => Math.max(44, p.name.length * 7 + 16);

/** A point. */
export interface Pt {
  x: number;
  y: number;
}

/** A rectangle. */
export interface Rect extends Pt {
  w: number;
  h: number;
}

/** A pin with its place on the drawing. */
export type PlacedPin = DiagramPin & Pt;

/** A box with its place and its pins'. */
export interface Box extends Rect {
  part: DiagramPart;
  pins: PlacedPin[];
  /** Where its group's content starts: saved positions are relative to it. */
  origin?: Pt;
}

/** A wire (an SVG path) with an optional net label. */
export interface Wire {
  net: string;
  path: string;
  label?: { x: number; y: number; anchor: 'start' | 'middle' | 'end'; text: string };
  arrow?: boolean;
  /** A passive's or ground pin's own short wire. */
  thin?: boolean;
}

/** A passive with its place. */
export interface PlacedPassive extends Rect {
  passive: DiagramPassive;
  /** Where its group's content starts: saved positions are relative to it. */
  origin?: Pt;
}

/** A group's frame, with what's written at its top. */
export interface Frame extends Rect {
  group: DiagramGroup;
  /** Height of the header with its name, description and notes. */
  header: number;
}

/** A net's name at the end of a passive whose other end isn't ground (a pull-up's 3V3). */
export interface Flag extends Pt {
  text: string;
  /** Above the passive (a rail), else below it. */
  up: boolean;
}

export interface BlockDrawing {
  boxes: Box[];
  passives: PlacedPassive[];
  wires: Wire[];
  /** Ground symbols, by the top of their stub. */
  grounds: Pt[];
  /** Net names at the far end of pull-ups and the like. */
  flags: Flag[];
  /** Junctions: where a wire branches. */
  dots: Pt[];
  /** Group frames, behind everything else. */
  frames: Frame[];
  width: number;
  height: number;
}

/** The layout engine, loaded on first use (it's 1.5 MB). */
let engine: Promise<ELK> | undefined;
function elk(): Promise<ELK> {
  engine ??= import('elkjs/lib/elk.bundled.js').then((m) => new m.default());
  return engine;
}

/** Lays out a block diagram: whole, or a group at a time. */
export function layoutBlockDiagram(d: Diagram): Promise<BlockDrawing> {
  return d.groups?.length ? layoutGroups(d) : layoutOne(d);
}

// ── groups ────────────────────────────────────────────────────────────

const GROUP_GAP = 60; // between group frames
const HEADER_LINE = 20; // a description or notes line in a frame's header

/** A frame's header height: its name, then its description and notes. */
export function headerHeight(g: DiagramGroup): number {
  return 34 + (g.description ? HEADER_LINE : 0) + (g.notes ? HEADER_LINE : 0);
}

/** The part of a diagram in one group ("" for none), its columns renumbered from 0. */
export function groupDiagram(d: Diagram, group: string): Diagram {
  const inGroup = (g?: string) => (g ?? '').toLowerCase() === group.toLowerCase();
  const parts = d.parts.filter((p) => inGroup(p.group));
  const columns = [...new Set(parts.map((p) => p.column))].sort((a, b) => a - b);
  return {
    parts: parts.map((p) => ({ ...p, column: columns.indexOf(p.column) })),
    nets: d.nets.filter((n) => inGroup(n.group)),
    passives: d.passives.filter((q) => inGroup(q.group)),
    groups: [],
    links: [],
  };
}

/** Moves an SVG path of absolute M, L, H and V commands. */
export function movePath(path: string, dx: number, dy: number): string {
  return path.replace(/([MLHV])(-?[\d.]+)(?:,(-?[\d.]+))?/g, (_, cmd: string, a: string, b?: string) => {
    if (cmd === 'H') return `H${Number(a) + dx}`;
    if (cmd === 'V') return `V${Number(a) + dy}`;
    return `${cmd}${Number(a) + dx},${Number(b) + dy}`;
  });
}

/** The top-left corner of everything drawn: boxes with their titles, passives, wires and their labels, symbols. */
export function topLeft(r: BlockDrawing): Pt {
  const pts: Pt[] = [
    ...r.boxes.map((b) => ({ x: b.x, y: b.y - TITLE })),
    ...r.passives,
    ...r.grounds,
    ...r.flags.map((f) => ({ x: f.x - f.text.length * 3.6, y: f.y - 12 })),
    ...r.dots,
    ...r.wires.flatMap((w) => [
      ...[...w.path.matchAll(/([MLHV])(-?[\d.]+)(?:,(-?[\d.]+))?/g)].map(([, c, a, b]) => (c === 'V' ? { x: Infinity, y: Number(a) } : { x: Number(a), y: c === 'H' ? Infinity : Number(b) })),
      ...(w.label ? [labelRect(w.label)] : []),
    ]),
  ];
  return { x: Math.min(...pts.map((p) => p.x)), y: Math.min(...pts.map((p) => p.y)) };
}

/** Moves a whole drawing by (dx, dy). */
export function moveDrawing(r: BlockDrawing, dx: number, dy: number): BlockDrawing {
  const pt = <T extends Pt>(p: T): T => ({ ...p, x: p.x + dx, y: p.y + dy });
  return {
    ...r,
    boxes: r.boxes.map((b) => ({ ...pt(b), pins: b.pins.map(pt) })),
    passives: r.passives.map(pt),
    wires: r.wires.map((w) => ({ ...w, path: movePath(w.path, dx, dy), label: w.label && pt(w.label) })),
    grounds: r.grounds.map(pt),
    flags: r.flags.map(pt),
    dots: r.dots.map(pt),
    frames: r.frames.map(pt),
  };
}

/**
 * Lays out each group on its own, framed, left to right in the backend's
 * order, then the parts in no group. Saved positions are relative to
 * their group's content, so moving a part in one group never moves
 * another group's parts.
 */
async function layoutGroups(d: Diagram): Promise<BlockDrawing> {
  const out: BlockDrawing = { boxes: [], passives: [], wires: [], grounds: [], flags: [], dots: [], frames: [], width: 0, height: 0 };
  const clusters: (DiagramGroup | undefined)[] = [...d.groups, undefined];
  const subs = clusters.map((group) => groupDiagram(d, group?.name ?? ''));
  const drawings = await Promise.all(subs.map((sub) => (sub.parts.length ? layoutOne(sub) : undefined)));
  let x = 0;
  clusters.forEach((group, i) => {
    const drawn = drawings[i];
    if (!drawn) return;
    const header = group ? headerHeight(group) : 0;
    // What's drawn starts a margin inside the frame, however far its stubs
    // and passives reach out from the boxes.
    const corner = topLeft(drawn);
    const dx = x + PAD - Math.min(PAD, corner.x);
    const dy = header + PAD - Math.min(PAD, corner.y);
    const origin = { x: dx, y: dy };
    const local = moveDrawing(drawn, dx, dy);
    local.width = drawn.width + (dx - x);
    local.height = drawn.height + (dy - header);
    // Boxes keep their own column (the group's layout renumbers them).
    const column = new Map(d.parts.map((p) => [p.id, p.column]));
    local.boxes = local.boxes.map((b) => ({ ...b, part: { ...b.part, column: column.get(b.part.id) ?? b.part.column } }));
    // The frame fits what's drawn, and the header's text.
    const textWidth = group ? 24 + 7.2 * Math.max(group.name.length * 1.15, (group.description ?? '').length, (group.notes ?? '').length + 7) : 0;
    const w = Math.max(local.width, textWidth);
    if (group) out.frames.push({ x: x + 12, y: 12, w: w - 24, h: header + local.height - 24, group, header });
    out.boxes.push(...local.boxes.map((b) => ({ ...b, origin })));
    out.passives.push(...local.passives.map((p) => ({ ...p, origin })));
    out.wires.push(...local.wires);
    out.grounds.push(...local.grounds);
    out.flags.push(...local.flags);
    out.dots.push(...local.dots);
    out.width = x + w;
    out.height = Math.max(out.height, header + local.height);
    x += w + GROUP_GAP;
  });
  return out;
}

// ── measure ───────────────────────────────────────────────────────────

/** Sizes each box: tall enough for its pins, wide enough for its bottom pins. */
export function measure(d: Diagram): Box[] {
  return d.parts.map((part) => {
    const pins = part.pins.map((p) => ({ ...p, x: 0, y: 0 }));
    // A pin tied to another of the part's pins (EN to VIN) is drawn on
    // the same side, just below it, joined to its wire.
    for (const p of pins.filter((x) => x.side === 'inside' && x.tied)) {
      const to = pins.find((x) => x.name === p.tied && x.side !== 'inside' && !x.ground);
      if (to && (to.side === 'left' || to.side === 'right')) p.side = to.side;
    }
    const left = pins.filter((p) => p.side === 'left' || p.side === 'inside').length;
    const right = pins.filter((p) => p.side === 'right').length;
    const bottom = pins.filter((p) => p.side === 'bottom' && !p.ground).length;
    const grounds = pins.filter((p) => p.ground);
    const w = Math.max(BOX_W, 24 + bottom * SLOT + grounds.reduce((n, p) => n + groundSlot(p), 0) + 24);
    const h = Math.max(120, TOP + Math.max(left, right) * PITCH + (bottom || grounds.length ? 44 : 10));
    return { part, pins, x: 0, y: 0, w, h };
  });
}

/**
 * Puts each pin on its side of a box at the origin, in the backend's
 * order: left (then inside ones below), right, bottom (ground pins last,
 * closer together). A pin tied to another comes right after it.
 */
export function pinsAt(b: Box): void {
  const withTies = (side: 'left' | 'right') => {
    const own = b.pins.filter((p) => p.side === side && !p.tied);
    const tied = b.pins.filter((p) => p.side === side && p.tied);
    const rest = tied.filter((t) => !own.some((p) => p.name === t.tied));
    return [...own.flatMap((p) => [p, ...tied.filter((t) => t.tied === p.name)]), ...rest];
  };
  [...withTies('left'), ...b.pins.filter((p) => p.side === 'inside')].forEach((p, i) => Object.assign(p, { x: b.x, y: b.y + TOP + i * PITCH }));
  withTies('right').forEach((p, i) => Object.assign(p, { x: b.x + b.w, y: b.y + TOP + i * PITCH }));
  const bottom = b.pins.filter((p) => p.side === 'bottom' && !p.ground);
  bottom.forEach((p, i) => Object.assign(p, { x: b.x + 50 + i * SLOT, y: b.y + b.h }));
  // Ground pins from the right, each as wide as its name.
  let x = b.x + b.w - 12;
  for (const p of b.pins.filter((g) => g.ground).reverse()) {
    x -= groundSlot(p) / 2;
    Object.assign(p, { x, y: b.y + b.h });
    x -= groundSlot(p) / 2;
  }
}

/** A passive's size from its text: its name and value, no more. */
export function passiveSize(p: DiagramPassive): { w: number; h: number } {
  return { w: Math.ceil(Math.max(52, CHAR * Math.max(p.label.length, (p.value ?? '').length) + 18) / 2) * 2, h: p.value ? 40 : 26 };
}

// ── graph ─────────────────────────────────────────────────────────────

/** How a passive's far end is shown: a ground symbol, or the net it goes to. */
type End = { ground: true } | { ground: false; text: string; up: boolean };

/** A passive as a node: its size and its far end. */
interface PassiveNode {
  passive: DiagramPassive;
  node: ElkNode;
  w: number;
  h: number;
  /** Shown at its far end, for a passive with one wired net. */
  end?: End;
}

/** A passive hung under a bottom pin of a box, not laid out by ELK. */
interface BelowPassive {
  passive: DiagramPassive;
  box: Box;
  pin: PlacedPin;
  w: number;
  h: number;
  end: End;
}

const DROP = 30; // from a bottom pin down to the passive under it

/** The name of a net leaving the group (or the board), as a node its stub ends at. */
interface FlagNode {
  net: string;
  node: ElkNode;
  /** The side of the pin it's drawn from: its text reads away from the pin. */
  side: 'left' | 'right';
  arrow?: boolean;
}

/** A pin's port on a net: the end of a wire. */
interface PortEnd {
  id: string;
  net: string;
  /** The side of its node it's on, which says which way the net flows. */
  side: 'WEST' | 'EAST' | 'SOUTH' | 'NORTH';
  /** On a passive, not a box. */
  passive?: boolean;
  /** On a series passive, which carries the net on to another. */
  series?: boolean;
}

/** The ELK graph of a diagram, and what each node stands for. */
interface Built {
  root: ElkNode;
  boxes: Map<string, Box>;
  passives: PassiveNode[];
  below: BelowPassive[];
  flags: FlagNode[];
  /** Nets whose wires carry a label (none when the net's name is a flag). */
  labelled: Set<string>;
  /** Edge ids that end in an arrow. */
  arrows: Set<string>;
}

const option = {
  layered: {
    'elk.algorithm': 'layered',
    'elk.direction': 'RIGHT',
    'elk.edgeRouting': 'ORTHOGONAL',
    'elk.layered.mergeEdges': 'true',
    'elk.layered.thoroughness': '50',
    'elk.layered.crossingMinimization.greedySwitch.type': 'TWO_SIDED',
    'elk.layered.nodePlacement.strategy': 'NETWORK_SIMPLEX',
    'elk.layered.nodePlacement.favorStraightEdges': 'true',
    'elk.layered.unnecessaryBendpoints': 'true',
    'elk.spacing.nodeNode': '44',
    'elk.spacing.edgeNode': '24',
    'elk.spacing.edgeEdge': '14',
    'elk.spacing.edgeLabel': '6',
    'elk.layered.spacing.nodeNodeBetweenLayers': '90',
    'elk.layered.spacing.edgeNodeBetweenLayers': '24',
    'elk.layered.spacing.edgeEdgeBetweenLayers': '14',
    'elk.edgeLabels.placement': 'CENTER',
    'elk.layered.edgeLabels.sideSelection': 'ALWAYS_UP',
    'elk.padding': `[top=${PAD},left=${PAD},bottom=${PAD},right=${PAD}]`,
  },
  /** With positions as hints: layers and order follow them. */
  interactive: {
    'elk.interactive': 'true',
    'elk.layered.cycleBreaking.strategy': 'INTERACTIVE',
    'elk.layered.layering.strategy': 'INTERACTIVE',
    'elk.layered.crossingMinimization.strategy': 'INTERACTIVE',
  },
};

/**
 * Room a node keeps clear above and below itself (a box's title, the
 * ground symbols and passives under its pins): ELK reserves it for
 * labels placed outside the node, so that's what these are.
 */
function room(width: number, above: number, below: number): Pick<ElkNode, 'labels' | 'layoutOptions'> {
  const place = (where: 'TOP' | 'BOTTOM') => ({ 'elk.nodeLabels.placement': `[H_CENTER, V_${where}, OUTSIDE]` });
  const labels = [];
  if (above > 0) labels.push({ text: ' ', width, height: above, layoutOptions: place('TOP') });
  if (below > 0) labels.push({ text: ' ', width, height: below, layoutOptions: place('BOTTOM') });
  return { labels, layoutOptions: place('TOP') };
}

/**
 * Builds the ELK graph of a diagram. Every pin on a side of a box is a
 * port at its place. A net's edges go from the port that drives it (a
 * right-side pin, or a series passive's far end; else the first pin on
 * it) to each other port on it; ELK merges them into a bus.
 */
function graph(d: Diagram, boxes: Box[]): Built {
  const built: Built = { root: { id: 'root', layoutOptions: option.layered, children: [], edges: [] }, boxes: new Map(), passives: [], below: [], flags: [], labelled: new Set(), arrows: new Set() };
  const children = built.root.children!;
  const edges = built.root.edges!;
  const ends = new Map<string, PortEnd[]>(); // net (lower case) → its ports
  const endsOf = (net: string) => ends.get(net.toLowerCase()) ?? [];
  const addEnd = (e: PortEnd) => ends.set(e.net.toLowerCase(), [...endsOf(e.net), e]);
  const kinds = new Map(d.nets.map((n) => [n.name.toLowerCase(), n.kind]));

  // A passive on a bottom pin alone (a configuration resistor) hangs
  // under that pin, not laid out by ELK: the box keeps room for it.
  const under = (b: Box, p: PlacedPin) => d.passives.find((q) => q.mount === 'below' && q.part === b.part.id && q.nets[0] === p.net);

  // Boxes, with their pins as ports on their sides: ELK orders the pins
  // on a side to keep their wires from crossing. A port is a pin's slot,
  // a pitch tall plus one for each pin tied to it below; its wire leaves
  // from the slot's first pitch.
  for (const b of boxes) {
    const id = `b:${b.part.id}`;
    built.boxes.set(id, b);
    const ports: ElkPort[] = [];
    let bottom = b.pins.some((p) => p.ground) ? GROUND + 10 : 6;
    b.pins.forEach((p, i) => {
      if (p.side === 'inside' || p.tied) return;
      const q = p.side === 'bottom' && !p.ground ? under(b, p) : undefined;
      if (q) {
        const { w, h } = passiveSize(q);
        built.below.push({ passive: q, box: b, pin: p, w, h, end: q.nets[1] ? { ground: false, text: q.nets[1], up: false } : { ground: true } });
        bottom = Math.max(bottom, DROP + h + GROUND + 14);
      }
      const side = p.side === 'left' ? 'WEST' : p.side === 'right' ? 'EAST' : 'SOUTH';
      const ties = b.pins.filter((t) => t.tied === p.name && t.side === p.side).length;
      const slot = side === 'SOUTH' ? { width: p.ground ? groundSlot(p) : Math.max(SLOT, (q ? passiveSize(q).w : 0) + 16), height: 0 } : { width: 0, height: PITCH * (1 + ties) };
      const anchor = side === 'SOUTH' ? `(${slot.width / 2},0)` : `(0,${PITCH / 2})`;
      ports.push({ id: `${id}/${i}`, ...slot, layoutOptions: { 'elk.port.side': side, 'elk.port.anchor': anchor } });
      if (!p.ground && !q) addEnd({ id: `${id}/${i}`, net: p.net, side });
    });
    const r = room(b.w, TITLE + 4, bottom);
    children.push({ id, width: b.w, height: b.h, ports, labels: r.labels, layoutOptions: { ...r.layoutOptions, 'elk.portConstraints': 'FIXED_SIDE', 'elk.portAlignment.default': 'CENTER', 'elk.spacing.portPort': '0' } });
  }

  // Passives: a bridge across two pins of one box, a series part between
  // two nets that reach different boxes, else hanging from one net with
  // its other end shown (ground, or the net's name).
  const boxesOn = (net: string) => new Set(endsOf(net).map((e) => e.id.split('/')[0]));
  for (const q of d.passives) {
    if (built.below.some((u) => u.passive === q)) continue;
    const { w, h } = passiveSize(q);
    const id = `q:${q.id}`;
    const nets = q.nets.filter((n) => boxesOn(n).size > 0);
    if (nets.length === 0) continue; // between passives only: not drawn
    const layoutOptions = { 'elk.portConstraints': 'FIXED_POS' };
    if (q.mount === 'bridge' && q.nets.length === 2) {
      const box = boxes.find((b) => b.part.id === q.part);
      const pins = q.nets.map((n) => box?.pins.find((p) => p.net === n && p.side !== 'inside' && !p.tied)).filter((p): p is PlacedPin => !!p);
      if (pins.length === 2) {
        const dy = Math.abs(pins[0].y - pins[1].y);
        const node: ElkNode = { id, width: 58, height: Math.max(44, dy + 30), layoutOptions, ports: [], labels: [] };
        q.nets.forEach((n, i) => {
          node.ports!.push({ id: `${id}/${i}`, x: 0, y: 15 + (pins[i].y > pins[1 - i].y ? dy : 0), width: 0, height: 0, layoutOptions: { 'elk.port.side': 'WEST' } });
          addEnd({ id: `${id}/${i}`, net: n, side: 'WEST', passive: true });
        });
        children.push(node);
        built.passives.push({ passive: q, node, w: 58, h: node.height! });
        continue;
      }
    }
    const power = (n: string) => kinds.get(n.toLowerCase()) === 'power';
    const pullUp = q.nets.length === 2 && power(q.nets[0]) !== power(q.nets[1]);
    const series = !pullUp && nets.length === 2 && !setsEqual(boxesOn(nets[0]), boxesOn(nets[1]));
    if (series) {
      // Upstream is the net something drives (a right-side pin), else
      // the one not at the part it serves.
      const driven = (n: string) => endsOf(n).some((e) => e.side === 'EAST');
      const [a, z] = driven(nets[1]) && !driven(nets[0]) ? [nets[1], nets[0]] : !driven(nets[0]) && boxesOn(nets[0]).has(`b:${q.supports}`) ? [nets[1], nets[0]] : [nets[0], nets[1]];
      const node: ElkNode = { id, width: w, height: h, layoutOptions, ports: [] };
      node.ports!.push({ id: `${id}/0`, x: 0, y: h / 2, width: 0, height: 0, layoutOptions: { 'elk.port.side': 'WEST' } });
      node.ports!.push({ id: `${id}/1`, x: w, y: h / 2, width: 0, height: 0, layoutOptions: { 'elk.port.side': 'EAST' } });
      addEnd({ id: `${id}/0`, net: a, side: 'WEST', passive: true, series: true });
      addEnd({ id: `${id}/1`, net: z, side: 'EAST', passive: true, series: true });
      children.push(node);
      built.passives.push({ passive: q, node, w, h });
      continue;
    }
    // Hanging from one net: a pull-up from the signal, its rail's name
    // above; else from the first net drawn, the other's name below.
    const rail = pullUp ? q.nets.find(power)! : undefined;
    const net = pullUp ? q.nets.find((n) => n !== rail)! : nets[0];
    const end: End = q.nets.length === 2 ? { ground: false, text: q.nets.find((n) => n !== net)!, up: pullUp } : { ground: true };
    const r = room(w, pullUp ? 20 : 0, pullUp ? 0 : GROUND + 10);
    const node: ElkNode = { id, width: w, height: h, layoutOptions: { ...layoutOptions, ...r.layoutOptions }, labels: r.labels, ports: [] };
    const side = pullUp ? 'SOUTH' : 'NORTH';
    node.ports!.push({ id: `${id}/0`, x: w / 2, y: pullUp ? h : 0, width: 0, height: 0, layoutOptions: { 'elk.port.side': side } });
    addEnd({ id: `${id}/0`, net, side, passive: true });
    children.push(node);
    built.passives.push({ passive: q, node, w, h, end });
  }

  // Nets: the name of one leaving the group is a node its stub ends at,
  // on the side of the pin it leaves from; one driven from outside the
  // group drives from that name.
  for (const n of d.nets) {
    const ports = endsOf(n.name);
    if (ports.length === 0) continue;
    const boxPorts = ports.filter((e) => !e.passive);
    // A net on one part is named at its stub, unless a series passive
    // carries it on to another part.
    const fromPassive = ports.find((e) => e.passive && e.side === 'EAST');
    const flagged = n.offboard || (n.wire === 'out' && !ports.some((e) => e.series));
    let source = fromPassive ?? boxPorts.find((e) => e.side === 'EAST') ?? boxPorts.find((e) => e.side === 'SOUTH');
    if (flagged) {
      const id = `f:${n.name}`;
      const fromRight = !!source;
      const node: ElkNode = { id, width: n.name.length * CHAR + 8, height: 14, layoutOptions: { 'elk.portConstraints': 'FIXED_POS' }, ports: [] };
      node.ports!.push({ id: `${id}/0`, x: fromRight ? 0 : node.width, y: 7, width: 0, height: 0, layoutOptions: { 'elk.port.side': fromRight ? 'WEST' : 'EAST' } });
      children.push(node);
      built.flags.push({ net: n.name, node, side: fromRight ? 'right' : 'left', arrow: fromRight && n.arrow });
      const flag: PortEnd = { id: `${id}/0`, net: n.name, side: fromRight ? 'WEST' : 'EAST' };
      if (fromRight) ports.push(flag);
      else source = flag;
    }
    source ??= boxPorts[0] ?? ports[0];
    const src = source;
    const sinks = ports.filter((e) => e !== src);
    sinks.forEach((e, i) => {
      const edge: ElkExtendedEdge = { id: `e:${n.name}:${i}`, sources: [src.id], targets: [e.id] };
      if (e.id.startsWith('f:') && built.flags.find((f) => f.node.id === e.id.split('/')[0])?.arrow) built.arrows.add(edge.id);
      // The first wire between two boxes carries the net's name, unless
      // a flag already says it.
      if (!flagged && !built.labelled.has(n.name) && !src.passive && !e.passive) {
        edge.labels = [{ text: n.name, width: n.name.length * CHAR, height: 14 }];
        built.labelled.add(n.name);
      }
      edges.push(edge);
    });
  }
  return built;
}

/** Whether two sets hold the same members. */
function setsEqual(a: Set<string>, b: Set<string>): boolean {
  return a.size === b.size && [...a].every((x) => b.has(x));
}

// ── layout ────────────────────────────────────────────────────────────

/**
 * Lays out a diagram (or one group of it) as one drawing. Saved positions
 * are hints: ELK lays the board out, each moved part is put where it was
 * dragged, and ELK lays it out again keeping that order.
 */
async function layoutOne(d: Diagram): Promise<BlockDrawing> {
  const boxes = measure(d);
  boxes.forEach(pinsAt);
  const built = graph(d, boxes);
  const engine = await elk();
  let r = await engine.layout(built.root);
  const hint = new Map<string, Pt>();
  for (const p of d.parts) if (p.x !== undefined && p.y !== undefined) hint.set(`b:${p.id}`, { x: p.x, y: p.y });
  for (const q of d.passives) if (q.x !== undefined && q.y !== undefined) hint.set(`q:${q.id}`, { x: q.x, y: q.y });
  if (hint.size) {
    for (const n of r.children ?? []) {
      const h = hint.get(n.id);
      if (h) [n.x, n.y] = [h.x, h.y];
      n.layoutOptions = { ...n.layoutOptions, 'elk.position': `(${n.x},${n.y})` };
    }
    r = await engine.layout({ ...r, layoutOptions: { ...option.layered, ...option.interactive } });
  }
  return drawing(built, r);
}

/** Rounds a coordinate to half a pixel, so right angles stay exact. */
const at = (v: number) => Math.round(v * 2) / 2;

/** Turns ELK's result into the drawing. */
function drawing(built: Built, r: ElkNode): BlockDrawing {
  const out: BlockDrawing = { boxes: [], passives: [], wires: [], grounds: [], flags: [], dots: [], frames: [], width: at(r.width ?? 0), height: at(r.height ?? 0) };
  const placed = new Map((r.children ?? []).map((n) => [n.id, n]));
  // Boxes: at ELK's place, their pins where ELK put the ports; a tied
  // pin right under the pin it's tied to, an inside pin after the left
  // ones.
  for (const [id, b] of built.boxes) {
    const n = placed.get(id)!;
    b.x = at(n.x ?? 0);
    b.y = at(n.y ?? 0);
    const ports = new Map((n.ports ?? []).map((port) => [port.id, port]));
    b.pins.forEach((p, i) => {
      const port = ports.get(`${id}/${i}`);
      if (!port) return;
      const [ax, ay] = p.side === 'bottom' ? [(port.width ?? 0) / 2, 0] : [0, PITCH / 2];
      p.x = at(b.x + (port.x ?? 0) + ax);
      p.y = at(b.y + (port.y ?? 0) + ay);
    });
    for (const p of b.pins.filter((x) => x.tied && (x.side === 'left' || x.side === 'right'))) {
      const to = b.pins.find((x) => x.name === p.tied && !x.tied && x.side === p.side);
      if (!to) continue;
      const k = b.pins.filter((x) => x.tied === p.tied && x.side === p.side).indexOf(p);
      p.x = to.x;
      p.y = to.y + (k + 1) * PITCH;
    }
    const left = b.pins.filter((p) => p.side === 'left');
    b.pins.filter((p) => p.side === 'inside').forEach((p, k) => Object.assign(p, { x: b.x, y: b.y + TOP + (left.length + k) * PITCH }));
    out.boxes.push(b);
    // Ground pins: a short stub to a ground symbol each. A tied pin:
    // out, and along to the wire of the pin it's tied to.
    for (const p of b.pins) {
      if (p.ground) {
        out.wires.push({ net: p.net, path: `M${p.x},${p.y} V${p.y + 8}`, thin: true });
        out.grounds.push({ x: p.x, y: p.y + 8 });
      } else if (p.tied && (p.side === 'left' || p.side === 'right')) {
        const to = b.pins.find((x) => x.name === p.tied && !x.tied);
        if (!to) continue;
        const x = p.x + (p.side === 'right' ? 10 : -10);
        out.wires.push({ net: p.net, path: `M${p.x},${p.y} H${x} V${to.y}` });
        out.dots.push({ x, y: to.y });
      }
    }
  }
  // Passives, with their far end below (or a pull-up's rail above).
  for (const q of built.passives) {
    const n = placed.get(q.node.id)!;
    const p: PlacedPassive = { passive: q.passive, x: at(n.x ?? 0), y: at(n.y ?? 0), w: q.w, h: q.h };
    out.passives.push(p);
    if (!q.end) continue;
    const cx = p.x + p.w / 2;
    if (q.end.ground) {
      out.wires.push({ net: 'GND', path: `M${cx},${p.y + p.h} V${p.y + p.h + 6}`, thin: true });
      out.grounds.push({ x: cx, y: p.y + p.h + 6 });
    } else {
      const y = q.end.up ? p.y : p.y + p.h;
      out.wires.push({ net: q.end.text, path: `M${cx},${y} V${y + (q.end.up ? -6 : 6)}`, thin: true });
      out.flags.push({ x: cx, y: y + (q.end.up ? -6 : 6), text: q.end.text, up: q.end.up });
    }
  }
  // Passives under a bottom pin, each on its own drop to the pin.
  for (const u of built.below) {
    const p: PlacedPassive = { passive: u.passive, x: at(u.pin.x - u.w / 2), y: at(u.pin.y + DROP), w: u.w, h: u.h };
    out.passives.push(p);
    out.wires.push({ net: u.passive.nets[0], path: `M${u.pin.x},${u.pin.y} V${p.y}`, thin: true });
    const cx = p.x + p.w / 2;
    if (u.end.ground) {
      out.wires.push({ net: 'GND', path: `M${cx},${p.y + p.h} V${p.y + p.h + 6}`, thin: true });
      out.grounds.push({ x: cx, y: p.y + p.h + 6 });
    } else {
      out.wires.push({ net: u.end.text, path: `M${cx},${p.y + p.h} V${p.y + p.h + 6}`, thin: true });
      out.flags.push({ x: cx, y: p.y + p.h + 6, text: u.end.text, up: false });
    }
  }
  // Wires, with their labels and junctions; a flag's name on its stub.
  const flagOf = new Map(built.flags.map((f) => [f.node.id, f]));
  const named = new Set<string>(); // flags already written on a wire
  for (const e of r.edges ?? []) {
    const net = e.id.slice(2, e.id.lastIndexOf(':'));
    const flag = flagOf.get(e.targets[0].split('/')[0]) ?? flagOf.get(e.sources[0].split('/')[0]);
    // The flag's name is written on the first wire that reaches it.
    const names = !!flag && !named.has(flag.node.id);
    if (flag) named.add(flag.node.id);
    (e.sections ?? []).forEach((s, k) => {
      const pts = [s.startPoint, ...(s.bendPoints ?? []), s.endPoint].map((p) => ({ x: at(p.x), y: at(p.y) }));
      const wire: Wire = { net, path: pts.map((p, i) => `${i ? 'L' : 'M'}${p.x},${p.y}`).join(' '), arrow: built.arrows.has(e.id) || undefined };
      out.wires.push(wire);
      if (k > 0) return;
      const l = e.labels?.[0];
      if (l && l.x !== undefined && l.y !== undefined) wire.label = { x: at(l.x + (l.width ?? 0) / 2), y: at(l.y + 11), anchor: 'middle', text: l.text ?? '' };
      if (flag && names) {
        const f = placed.get(flag.node.id)!;
        const fx = at(f.x ?? 0);
        wire.label = flag.side === 'right' ? { x: fx + 2, y: at((f.y ?? 0) + 11), anchor: 'start', text: flag.net } : { x: at(fx + (f.width ?? 0)) - 2, y: at((f.y ?? 0) + 11), anchor: 'end', text: flag.net };
      }
    });
    for (const j of e.junctionPoints ?? []) out.dots.push({ x: at(j.x), y: at(j.y) });
  }
  // As big as everything drawn, with a margin: a passive under a bottom
  // pin can reach past ELK's box.
  const extents = [
    ...out.boxes.map((b) => ({ x: b.x + b.w, y: b.y + b.h + GROUND })),
    ...out.passives.map((p) => ({ x: p.x + p.w, y: p.y + p.h + GROUND })),
    ...out.grounds.map((g) => ({ x: g.x + 8, y: g.y + GROUND })),
    ...out.wires.map(wireExtent),
  ];
  out.width = Math.max(out.width, ...extents.map((e) => e.x + PAD));
  out.height = Math.max(out.height, ...extents.map((e) => e.y + PAD));
  return out;
}

/** The far corner of a wire, with its label. */
function wireExtent(w: Wire): Pt {
  const xs = [...w.path.matchAll(/(?:[ML](-?[\d.]+),|H(-?[\d.]+))/g)].map((m) => Number(m[1] ?? m[2]));
  const ys = [...w.path.matchAll(/(?:,(-?[\d.]+)|V(-?[\d.]+))/g)].map((m) => Number(m[1] ?? m[2]));
  const l = w.label ? labelRect(w.label) : undefined;
  return { x: Math.max(0, ...xs, l ? l.x + l.w : 0), y: Math.max(0, ...ys, l ? l.y + l.h : 0) };
}

// ── helpers ───────────────────────────────────────────────────────────

/** Whether two rectangles overlap, with a margin. */
export function overlaps(a: Rect, b: Rect, margin = 0): boolean {
  return a.x < b.x + b.w + margin && b.x < a.x + a.w + margin && a.y < b.y + b.h + margin && b.y < a.y + a.h + margin;
}

/** Where a wire's label is drawn, roughly. */
export function labelRect(l: NonNullable<Wire['label']>): Rect {
  const w = l.text.length * CHAR;
  const x = l.anchor === 'middle' ? l.x - w / 2 : l.anchor === 'end' ? l.x - w : l.x;
  return { x, y: l.y - 12, w, h: 15 };
}
