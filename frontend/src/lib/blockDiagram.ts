// Places a project's block diagram. The backend decides what is drawn
// (core.Diagram: each box's pins and their sides, which group each box
// and passive is in, what each passive serves); this turns it into a
// graph for ELK (the Eclipse Layout Kernel, elkjs) and the result back
// into a drawing:
//
//   measure → each symbol's size from its pins, and where each pin is on it:
//             a body with pin lines out to the node's border, where wires
//             attach (a generated symbol shows every pin of its pin table,
//             unconnected ones dimmed; "connected pins only" hides those)
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
// flow order. A part dragged by hand is a hint to ELK: its order among
// the parts beside it follows where it was put (left to right stays with
// the flow), and the wires are routed again.
import type { ELK, ElkExtendedEdge, ElkNode, ElkPort } from 'elkjs/lib/elk-api';
import { threeTerminal } from './symbols';
import type { Diagram, DiagramGroup, DiagramPart, DiagramPassive, DiagramPin } from './types';

export const PITCH = 30; // between pins
export const TOP = 25; // first pin below a body's top
export const PIN_LEN = 20; // a pin line, from the body to where its wire attaches
export const BOX_W = 200;
export const PAD = 40;
const TITLE = 30; // a box's title above it
const GROUND = 16; // a ground symbol's height, with its stub
const SLOT = 84; // a bottom pin with a passive hanging under it
const CHAR = 7.5; // a label's width per character
const PIN_CHAR = 6.6; // a pin name's width per character (11px)

/** What the layout may be asked for. */
export interface LayoutOptions {
  /** Hide a generated symbol's unconnected pins, except supplies and grounds. */
  connectedOnly?: boolean;
}

/** The pins of a part that are drawn. */
export function visiblePins(part: DiagramPart, opts?: LayoutOptions): DiagramPin[] {
  if (!opts?.connectedOnly || !part.full) return part.pins;
  return part.pins.filter((p) => p.connected || p.role === 'power' || p.role === 'ground' || p.role === 'pad');
}

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

/** A pin with its place on the drawing: the end of its pin line, where its wire attaches. */
export type PlacedPin = DiagramPin & Pt & { dim?: boolean };

/**
 * A symbol with its place: the node (its body plus its pin lines, which
 * wires attach to at its border) and its pins'.
 */
export interface Box extends Rect {
  part: DiagramPart;
  pins: PlacedPin[];
  /** The rectangle the pins stick out of. */
  body: Rect;
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
  /** Its symbol's ends, where its wires attach: down the node, or along it when horizontal. */
  ends: [Pt, Pt];
  /** The terminal at each end (a diode's anode and cathode), as the backend names them. */
  terminals: [string, string];
  horizontal?: boolean;
  /** Where its name is written (its value under it): beside a vertical symbol, above a horizontal one. */
  label: Pt & { anchor: 'start' | 'middle' };
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
  /** Above the passive (the rail it hangs from), else below it. */
  up?: boolean;
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
export function layoutBlockDiagram(d: Diagram, opts: LayoutOptions = {}): Promise<BlockDrawing> {
  return d.groups?.length ? layoutGroups(d, opts) : layoutOne(d, opts);
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
    boxes: r.boxes.map((b) => ({ ...pt(b), body: pt(b.body), pins: b.pins.map(pt) })),
    passives: r.passives.map((p) => ({ ...pt(p), ends: [pt(p.ends[0]), pt(p.ends[1])] as [Pt, Pt], label: pt(p.label) })),
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
async function layoutGroups(d: Diagram, opts: LayoutOptions): Promise<BlockDrawing> {
  const out: BlockDrawing = { boxes: [], passives: [], wires: [], grounds: [], flags: [], dots: [], frames: [], width: 0, height: 0 };
  const clusters: (DiagramGroup | undefined)[] = [...d.groups, undefined];
  const subs = clusters.map((group) => groupDiagram(d, group?.name ?? ''));
  const drawings = await Promise.all(subs.map((sub) => (sub.parts.length ? layoutOne(sub, opts) : undefined)));
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

// ── measure ──────────────────────────────────────────────────────────

/** The room a bottom pin takes: a slot for a passive hanging under it, else a pitch. */
function bottomSlot(d: Diagram, part: DiagramPart, p: DiagramPin): number {
  const under = p.net && d.passives.some((q) => q.mount === 'below' && q.part === part.id && q.nets[0] === p.net);
  return under ? SLOT : PITCH;
}

/**
 * The pin a tied pin's jog runs to: the nearest untied pin of that name
 * on its side (a part may have several VDD pins, in more than one run).
 */
export function tiedTo(b: Box, p: PlacedPin): PlacedPin | undefined {
  const mains = b.pins.filter((x) => x.name === p.tied && !x.tied && x.side === p.side);
  const dist = (x: PlacedPin) => Math.abs(x.x - p.x) + Math.abs(x.y - p.y);
  return mains.sort((m, n) => dist(m) - dist(n))[0] ?? b.pins.find((x) => x.name === p.tied && !x.tied);
}

/** The width of the longest pin name among some pins. */
const longest = (ps: DiagramPin[]) => Math.max(0, ...ps.map((p) => p.name.length)) * PIN_CHAR;

/**
 * The bands a body keeps for its top and bottom pins' names (written
 * down the body): the left and right pins start below the top band and
 * end above the bottom one.
 */
export function bands(pins: DiagramPin[]): { top: number; bottom: number } {
  const top = pins.filter((p) => p.side === 'top');
  const bottom = pins.filter((p) => p.side === 'bottom');
  const even = (v: number) => Math.ceil(v / 2) * 2;
  return { top: top.length ? even(14 + longest(top)) : TOP, bottom: bottom.length ? even(14 + longest(bottom)) : 10 };
}

/**
 * Sizes each symbol: a body wide enough for the names of its left and
 * right pins and its top and bottom pins' pitch, tall enough for its
 * left and right pins' pitch between the bands its top and bottom pins'
 * names take; the node adds a pin line all round.
 */
export function measure(d: Diagram, opts: LayoutOptions = {}): Box[] {
  return d.parts.map((part) => {
    const pins: PlacedPin[] = visiblePins(part, opts).map((p) => ({ ...p, x: 0, y: 0, dim: !p.connected || undefined }));
    const tr = threeTerminal(part.shape);
    if (tr) {
      // A transistor is its symbol, the pin ends on the symbol's border.
      return { part, pins, body: { x: 0, y: 0, w: tr.w, h: tr.h }, x: 0, y: 0, w: tr.w, h: tr.h };
    }
    const on = (side: DiagramPin['side']) => pins.filter((p) => p.side === side);
    const bottomW = on('bottom').reduce((n, p) => n + bottomSlot(d, part, p), 0);
    const bodyW = Math.max(BOX_W - 2 * PIN_LEN, 12 + longest(on('left')) + 24 + longest(on('right')) + 12, 2 * TOP + on('top').length * PITCH, TOP + bottomW + TOP - PITCH);
    const band = bands(pins);
    const rows = Math.max(on('left').length, on('right').length);
    const bodyH = Math.max(120 - 2 * PIN_LEN, band.top + Math.max(0, rows - 1) * PITCH + band.bottom);
    const body = { x: PIN_LEN, y: PIN_LEN, w: Math.ceil(bodyW / 2) * 2, h: Math.ceil(bodyH / 2) * 2 };
    return { part, pins, body, x: 0, y: 0, w: body.w + 2 * PIN_LEN, h: body.h + 2 * PIN_LEN };
  });
}

/**
 * Puts each pin's end on the node's border, in the backend's order: left
 * and right pins down each side from the body's top, top and bottom pins
 * along from its left. On a box drawn from its connections, a pin tied
 * to another comes right after it; a generated symbol keeps its number
 * order (the backend ties only neighbours).
 */
export function pinsAt(b: Box, d?: Diagram): void {
  const tr = threeTerminal(b.part.shape);
  if (tr) {
    b.body = { x: b.x, y: b.y, w: b.w, h: b.h };
    for (const p of b.pins) {
      const t = tr.terminals[p.terminal ?? ''] ?? { x: 0, y: b.h / 2 };
      Object.assign(p, { x: b.x + t.x, y: b.y + t.y });
    }
    return;
  }
  const body = { x: b.x + PIN_LEN, y: b.y + PIN_LEN, w: b.w - 2 * PIN_LEN, h: b.h - 2 * PIN_LEN };
  b.body = body;
  const on = (side: DiagramPin['side']) => {
    const ps = b.pins.filter((p) => p.side === side);
    if (b.part.full) return ps;
    const own = ps.filter((p) => !p.tied);
    const tied = ps.filter((p) => p.tied);
    const rest = tied.filter((t) => !own.some((p) => p.name === t.tied));
    return [...own.flatMap((p) => [p, ...tied.filter((t) => t.tied === p.name)]), ...rest];
  };
  const top = bands(b.pins).top;
  on('left').forEach((p, i) => Object.assign(p, { x: b.x, y: body.y + top + i * PITCH }));
  on('right').forEach((p, i) => Object.assign(p, { x: b.x + b.w, y: body.y + top + i * PITCH }));
  on('top').forEach((p, i) => Object.assign(p, { x: body.x + TOP + i * PITCH, y: b.y }));
  let x = body.x + TOP;
  for (const p of on('bottom')) {
    const slot = d ? bottomSlot(d, b.part, p) : PITCH;
    Object.assign(p, { x: x + (slot - PITCH) / 2, y: b.y + b.h });
    x += slot;
  }
}

const SYM = 28; // the band a two-terminal symbol takes across its axis
const SYM_LEN = 44; // a two-terminal's node along its axis: the symbol and a short lead each end
const BRIDGE_IN = 4; // a bridging passive's ends, in from its node's top and bottom
/** The shapes drawn aside under their net's name rather than wired at a pin: what a person works, not what a pin needs. */
const ASIDE = new Set(['switch', 'led', 'battery']);

/** How wide a passive's name and value are. */
const passiveText = (p: DiagramPassive) => Math.ceil(CHAR * Math.max(p.label.length, (p.value ?? '').length)) + 6;

/**
 * A passive's node: its symbol down the node with its name beside it,
 * or, horizontal, along the node with its name above. `axis` is where
 * the symbol runs: x from the left when vertical, y from the top when
 * horizontal; its ends are at the node's border.
 */
export function passiveSize(p: DiagramPassive, horizontal = false): { w: number; h: number; axis: number } {
  const text = passiveText(p);
  if (horizontal) {
    const above = p.value ? 28 : 16;
    return { w: Math.max(SYM_LEN, text), h: above + SYM, axis: above + SYM / 2 };
  }
  return { w: SYM + text, h: SYM_LEN, axis: SYM / 2 };
}

/** Places a passive's node: its ends, which terminal is at each, and its label. */
function placePassive(q: DiagramPassive, x: number, y: number, size: { w: number; h: number; axis: number }, horizontal: boolean, firstNet: string): PlacedPassive {
  const ends: [Pt, Pt] = horizontal ? [{ x, y: y + size.axis }, { x: x + size.w, y: y + size.axis }] : [{ x: x + size.axis, y }, { x: x + size.axis, y: y + size.h }];
  const [a, z] = q.ends.length === 2 ? q.ends : ['1', '2'];
  const terminals: [string, string] = firstNet.toLowerCase() === q.nets[0].toLowerCase() ? [a, z] : [z, a];
  const label = horizontal ? { x: x + size.w / 2, y: y + 8, anchor: 'middle' as const } : { x: x + SYM + 2, y: y + size.h / 2 - (q.value ? 6 : 0), anchor: 'start' as const };
  return { passive: q, x, y, w: size.w, h: size.h, ends, terminals, horizontal: horizontal || undefined, label };
}

// ── graph ─────────────────────────────────────────────────────────────

/** How a passive's far end is shown: a ground symbol, or the net it goes to. */
type End = { ground: true } | { ground: false; text: string };

/** A passive as a node: its size, the net at its first end, and its far end. */
interface PassiveNode {
  passive: DiagramPassive;
  node: ElkNode;
  size: { w: number; h: number; axis: number };
  horizontal: boolean;
  /** The net at its first end (the top, or the left). */
  first: string;
  /** Bridging two pins: the side its ports are on, facing them. */
  bridge?: 'EAST' | 'WEST';
  /** Standing aside from its pin: its net's name is flagged above it instead of a wire. */
  flag?: string;
  /** Shown at its far end, for a passive with one wired net. */
  end?: End;
}

/** A passive hung under a bottom pin of a box, not laid out by ELK. */
interface BelowPassive {
  passive: DiagramPassive;
  box: Box;
  pin: PlacedPin;
  size: { w: number; h: number; axis: number };
  end: End;
}

const DROP = 30; // from a bottom pin down to the passive under it

/**
 * The name of a net leaving the group (or the board) at a pin nothing in
 * the group is wired to: a short stub out of the pin, its name at the end.
 * ELK keeps the room for it as the pin's port label.
 */
interface Stub {
  port: string;
  net: string;
  side: 'left' | 'right' | 'top' | 'bottom';
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
  d: Diagram;
  root: ElkNode;
  boxes: Map<string, Box>;
  passives: PassiveNode[];
  below: BelowPassive[];
  stubs: Stub[];
  /** Nets whose name is written somewhere already. */
  labelled: Set<string>;
  /** Edge ids that carry their net's name (written on the wire after layout). */
  named: Set<string>;
}

const option = {
  layered: {
    'elk.algorithm': 'layered',
    'elk.direction': 'RIGHT',
    'elk.edgeRouting': 'ORTHOGONAL',
    'elk.layered.mergeEdges': 'true',
    'elk.layered.thoroughness': '50',
    'elk.layered.crossingMinimization.greedySwitch.type': 'TWO_SIDED',
    'elk.layered.unnecessaryBendpoints': 'true',
    // Puts each node level with what it's wired to (shortest wires), so a
    // passive sits beside its pin rather than where a stack of them
    // happens to centre.
    'elk.layered.nodePlacement.strategy': 'NETWORK_SIMPLEX',
    // Nodes in a layer keep the order they're given where crossings allow:
    // boxes first, so passives hang below them rather than stacking above.
    'elk.layered.considerModelOrder.strategy': 'NODES_AND_EDGES',
    'elk.spacing.nodeNode': '44',
    'elk.spacing.edgeNode': '24',
    'elk.spacing.edgeEdge': '18',
    'elk.layered.spacing.nodeNodeBetweenLayers': '90',
    'elk.layered.spacing.edgeNodeBetweenLayers': '24',
    'elk.layered.spacing.edgeEdgeBetweenLayers': '18',
    'elk.padding': `[top=${PAD},left=${PAD},bottom=${PAD},right=${PAD}]`,
  },
  /**
   * With positions as hints: the order of the nodes in each layer follows
   * them. The layers themselves still follow the wires, so a part dragged
   * against the flow isn't pulled behind what feeds it.
   */
  interactive: {
    'elk.interactive': 'true',
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
  const built: Built = { d, root: { id: 'root', layoutOptions: option.layered, children: [], edges: [] }, boxes: new Map(), passives: [], below: [], stubs: [], labelled: new Set(), named: new Set() };
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
  // Pins are fixed ports at their place on the node's border: a
  // generated symbol's in number order, a box's in the backend's (by
  // where their wires go). ELK isn't asked to order them: it spreads
  // ports along a side regardless of the bands the names take.
  for (const b of boxes) {
    const id = `b:${b.part.id}`;
    built.boxes.set(id, b);
    const ports: ElkPort[] = [];
    let bottom = b.pins.some((p) => p.ground) ? GROUND + 10 : 6;
    b.pins.forEach((p, i) => {
      if (p.tied || p.dim) return;
      const q = p.side === 'bottom' && !p.ground ? under(b, p) : undefined;
      if (q) {
        const size = passiveSize(q);
        built.below.push({ passive: q, box: b, pin: p, size, end: q.nets[1] ? { ground: false, text: q.nets[1] } : { ground: true } });
        bottom = Math.max(bottom, DROP + size.h + GROUND + 14);
      }
      const side = p.side === 'left' ? 'WEST' : p.side === 'right' ? 'EAST' : p.side === 'top' ? 'NORTH' : 'SOUTH';
      ports.push({ id: `${id}/${i}`, x: p.x - b.x, y: p.y - b.y, width: 0, height: 0, layoutOptions: { 'elk.port.side': side } });
      if (!p.ground && !q && p.net) addEnd({ id: `${id}/${i}`, net: p.net, side });
    });
    const r = room(b.w, TITLE + 4, bottom);
    children.push({
      id,
      width: b.w,
      height: b.h,
      ports,
      labels: r.labels,
      layoutOptions: { ...r.layoutOptions, 'elk.portConstraints': 'FIXED_POS', 'elk.portLabels.placement': 'OUTSIDE' },
    });
  }

  // Passives: a bridge across two pins of one box, a series part between
  // two nets that reach different boxes, else hanging from one net with
  // its other end shown (ground, or the net's name).
  const boxesOn = (net: string) => new Set(endsOf(net).map((e) => e.id.split('/')[0]));
  for (const q of d.passives) {
    if (built.below.some((u) => u.passive === q)) continue;
    const id = `q:${q.id}`;
    const nets = q.nets.filter((n) => boxesOn(n).size > 0);
    if (nets.length === 0) continue; // between passives only: not drawn
    const layoutOptions = { 'elk.portConstraints': 'FIXED_POS' };
    if (q.mount === 'bridge' && q.nets.length === 2) {
      const box = boxes.find((b) => b.part.id === q.part);
      const pins = q.nets.map((n) => box?.pins.find((p) => p.net === n && !p.tied && !p.dim)).filter((p): p is PlacedPin => !!p);
      if (pins.length === 2) {
        // Beside the pins it bridges, its ports on the side facing them
        // and its symbol down the node just inside, a short lead from
        // each port; its name on the far side.
        const dy = Math.abs(pins[0].y - pins[1].y);
        const side = pins[0].side === 'left' ? 'EAST' : 'WEST';
        const size = { ...passiveSize(q), h: Math.max(SYM_LEN, dy + 30) };
        size.axis = side === 'EAST' ? size.w - SYM / 2 : SYM / 2;
        const node: ElkNode = { id, width: size.w, height: size.h, layoutOptions, ports: [], labels: [] };
        const upper = pins[0].y <= pins[1].y ? 0 : 1;
        q.nets.forEach((n, i) => {
          node.ports!.push({ id: `${id}/${i}`, x: side === 'EAST' ? size.w : 0, y: i === upper ? BRIDGE_IN : size.h - BRIDGE_IN, width: 0, height: 0, layoutOptions: { 'elk.port.side': side } });
          addEnd({ id: `${id}/${i}`, net: n, side, passive: true });
        });
        children.push(node);
        built.passives.push({ passive: q, node, size, horizontal: false, first: q.nets[upper], bridge: side });
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
      const size = passiveSize(q, true);
      const node: ElkNode = { id, width: size.w, height: size.h, layoutOptions, ports: [] };
      node.ports!.push({ id: `${id}/0`, x: 0, y: size.axis, width: 0, height: 0, layoutOptions: { 'elk.port.side': 'WEST' } });
      node.ports!.push({ id: `${id}/1`, x: size.w, y: size.axis, width: 0, height: 0, layoutOptions: { 'elk.port.side': 'EAST' } });
      addEnd({ id: `${id}/0`, net: a, side: 'WEST', passive: true, series: true });
      addEnd({ id: `${id}/1`, net: z, side: 'EAST', passive: true, series: true });
      children.push(node);
      built.passives.push({ passive: q, node, size, horizontal: true, first: a });
      continue;
    }
    // Hanging from one net, its far end named below: a pull-up from the
    // signal, with its rail's name; else from the first net drawn.
    const rail = pullUp ? q.nets.find(power)! : undefined;
    const net = pullUp ? q.nets.find((n) => n !== rail)! : nets[0];
    const end: End = q.nets.length === 2 ? { ground: false, text: q.nets.find((n) => n !== net)! } : { ground: true };
    const size = passiveSize(q);
    const r = room(size.w, 0, GROUND + 10);
    const node: ElkNode = { id, width: size.w, height: size.h, layoutOptions: { ...layoutOptions, ...r.layoutOptions }, labels: r.labels, ports: [] };
    node.ports!.push({ id: `${id}/0`, x: size.axis, y: 0, width: 0, height: 0, layoutOptions: { 'elk.port.side': 'NORTH' } });
    addEnd({ id: `${id}/0`, net, side: 'NORTH', passive: true });
    children.push(node);
    built.passives.push({ passive: q, node, size, horizontal: false, first: net, end });
  }

  // Nets. One leaving the group (or the board) from a pin nothing in the
  // group is wired to is a stub out of that pin, named; an output leaving
  // the board ends in an arrow, with what hangs off it branching from
  // the stub. A rail nothing in the group drives (3V3 in the MCU group,
  // made in the Power group) isn't wired at all, as on a schematic: each
  // pin on it gets a stub with its name, and each capacitor on it stands
  // in a row by the part it serves with the rail's name above it. The
  // rest are edges from the pin that drives the net to each other pin,
  // the first between two boxes (else the first at all) carrying its
  // name; with nothing driving it, each passive on it comes before the
  // pin it feeds (a pull-up left of an input), not after it at the end of
  // a wire round the box.
  const portOf = new Map<string, ElkPort>();
  for (const n of children) for (const port of n.ports ?? []) portOf.set(port.id, port);
  const stub = (e: PortEnd, net: string, arrow: boolean) => {
    const side = e.side === 'EAST' ? 'right' : e.side === 'NORTH' ? 'top' : e.side === 'SOUTH' ? 'bottom' : 'left';
    built.stubs.push({ port: e.id, net, side, arrow: side === 'right' && arrow });
    portOf.get(e.id)!.labels = [{ text: net, width: net.length * CHAR + 16 + (arrow ? 12 : 0), height: 14 }];
    built.labelled.add(net);
  };
  const column = new Map(d.parts.map((p) => [`b:${p.id}`, p.column]));
  const passiveOf = (e: PortEnd) => built.passives.find((x) => `${x.node.id}/0` === e.id);
  // Stands aside under its net's name rather than wired at the pin, placed
  // in a row by the part it serves (ghost edges, not drawn, chain the row).
  const aside = (e: PortEnd, net: string, by: PortEnd) => {
    const q = passiveOf(e);
    if (!q) return;
    q.flag = net;
    const row = built.passives.filter((x) => x.flag && x.passive.part === q.passive.part && x !== q);
    edges.push({ id: `g:${net}:${e.id}`, sources: [row.length ? `${row[row.length - 1].node.id}/0` : by.id], targets: [e.id] });
  };
  for (const n of d.nets) {
    const ports = endsOf(n.name);
    if (ports.length === 0) continue;
    const boxPorts = ports.filter((e) => !e.passive);
    const fromPassive = ports.find((e) => e.passive && e.side === 'EAST');
    const driver = fromPassive ?? boxPorts.find((e) => e.side === 'EAST') ?? boxPorts.find((e) => e.side === 'SOUTH');
    const series = ports.some((e) => e.series);
    const hung = ports.filter((e) => e.passive && !e.series);
    const rail = !driver && !series && kinds.get(n.name.toLowerCase()) === 'power' && boxPorts.length > 0;
    const serves = hung.map((e) => passiveOf(e)?.passive.supports).find(Boolean);
    const home = driver ?? boxPorts.find((e) => e.id.startsWith(`b:${serves}/`)) ?? boxPorts[0];
    // What stays wired at the pin: a crystal, its load capacitors and the
    // other passives of the pin's own net. A net with a switch, button,
    // LED or battery on it, or a rail, stands aside whole, under the net's
    // name: all of what hangs off it together, never half at the pin and
    // half away from it.
    const standsAside = rail || hung.some((e) => ASIDE.has(passiveOf(e)?.passive.shape ?? ''));
    if (home && standsAside) for (const e of hung) aside(e, n.name, home);
    const wiredPassives = standsAside ? [] : hung;
    // Which pins are labelled rather than wired: every pin of a rail; of
    // a signal reaching three parts, or two more than a column apart (a
    // long wire round everything between), every pin but the one its
    // passives are wired at; and any pin left with nothing wired to it.
    const boxIds = [...new Set(boxPorts.map((e) => e.id.split('/')[0]))];
    const far = !series && (boxIds.length >= 3 || (boxIds.length === 2 && Math.abs((column.get(boxIds[0]) ?? 0) - (column.get(boxIds[1]) ?? 0)) >= 2));
    const wiredPins = rail ? [] : far ? boxPorts.filter((e) => e === home && wiredPassives.length > 0) : boxPorts;
    const wired = [...wiredPins, ...wiredPassives, ...ports.filter((e) => e.series)];
    const leaves = n.offboard || (n.wire === 'out' && !series);
    for (const e of boxPorts) if (!wiredPins.includes(e) && e.side !== 'SOUTH') stub(e, n.name, false);
    // A pin alone on its net (the rest is in another group, or off the
    // board) gets a stub with its name; an output leaving the board ends
    // in an arrow, with what hangs off it branching from the stub.
    if (wired.length === 1 && !wired[0].passive && wired[0].side !== 'SOUTH') stub(wired[0], n.name, leaves && !!n.arrow);
    if (wired.length < 2) continue;
    if (leaves && n.arrow && driver && !driver.passive && driver.side !== 'SOUTH') stub(driver, n.name, true);
    // Edges from the pin that drives the net to each other end, the first
    // between two boxes (else the first at all) carrying its name; with
    // nothing driving it, each passive comes before the pin it feeds (a
    // pull-up left of an input), not after it at the end of a wire round
    // the box.
    const source = driver ?? wired.find((e) => e.passive) ?? wired[0];
    const sinks = wired.filter((e) => e !== source);
    const between = sinks.find((e) => !e.passive && !source.passive);
    const feeds = !driver && wiredPins[0];
    sinks.forEach((e, i) => {
      const fromPin = feeds && e.passive ? [e.id, feeds.id] : [source.id, e.id];
      const edge: ElkExtendedEdge = { id: `e:${n.name}:${i}`, sources: [fromPin[0]], targets: [fromPin[1]] };
      if (!built.labelled.has(n.name) && (e === between || (!between && i === 0))) {
        built.named.add(edge.id);
        built.labelled.add(n.name);
      }
      edges.push(edge);
    });
  }
  // Nodes in a layer keep this order where crossings allow: boxes first,
  // then the passives in the order of the pins they feed down each box,
  // so a crystal sits by its oscillator pins, not above the whole part.
  const boxIndex = new Map(children.filter((c) => c.id.startsWith('b:')).map((c, i) => [c.id, i]));
  const anchor = (c: ElkNode): [number, number] => {
    const q = built.passives.find((x) => x.node === c);
    for (const net of q?.passive.nets ?? []) {
      const e = endsOf(net).find((x) => !x.passive);
      if (e) return [boxIndex.get(e.id.split('/')[0]) ?? 0, portOf.get(e.id)?.y ?? 0];
    }
    return [Infinity, 0];
  };
  const rank = new Map(children.map((c) => [c.id, c.id.startsWith('b:') ? [-1, boxIndex.get(c.id) ?? 0] : anchor(c)]));
  children.sort((a, b) => {
    const [ia, ya] = rank.get(a.id)!;
    const [ib, yb] = rank.get(b.id)!;
    return ia - ib || ya - yb;
  });
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
 * dragged, and a fresh graph is laid out again keeping that order (a
 * fresh one: the first result's wires would be taken as given).
 */
async function layoutOne(d: Diagram, opts: LayoutOptions): Promise<BlockDrawing> {
  const boxes = measure(d, opts);
  boxes.forEach((b) => pinsAt(b, d));
  const engine = await elk();
  const first = graph(d, boxes);
  const r = await engine.layout(first.root);
  const hint = new Map<string, Pt>();
  for (const p of d.parts) if (p.x !== undefined && p.y !== undefined) hint.set(`b:${p.id}`, { x: p.x, y: p.y });
  for (const q of d.passives) if (q.x !== undefined && q.y !== undefined) hint.set(`q:${q.id}`, { x: q.x, y: q.y });
  if (!hint.size) return drawing(first, r);
  const was = new Map((r.children ?? []).map((n) => [n.id, { x: n.x ?? 0, y: n.y ?? 0 }]));
  const again = graph(d, boxes);
  for (const n of again.root.children ?? []) {
    const at = hint.get(n.id) ?? was.get(n.id);
    if (!at) continue;
    [n.x, n.y] = [at.x, at.y];
    n.layoutOptions = { ...n.layoutOptions, 'elk.position': `(${at.x},${at.y})` };
  }
  again.root.layoutOptions = { ...option.layered, ...option.interactive };
  return drawing(again, await engine.layout(again.root));
}

/** Rounds a coordinate to half a pixel, so right angles stay exact. */
const at = (v: number) => Math.round(v * 2) / 2;

/** Turns ELK's result into the drawing. */
function drawing(built: Built, r: ElkNode): BlockDrawing {
  const out: BlockDrawing = { boxes: [], passives: [], wires: [], grounds: [], flags: [], dots: [], frames: [], width: at(r.width ?? 0), height: at(r.height ?? 0) };
  const placed = new Map((r.children ?? []).map((n) => [n.id, n]));
  // Symbols: at ELK's place, their pins where they were measured (ELK
  // was given them as fixed ports).
  for (const [id, b] of built.boxes) {
    const n = placed.get(id)!;
    b.x = at(n.x ?? 0);
    b.y = at(n.y ?? 0);
    pinsAt(b, built.d);
    out.boxes.push(b);
    // Ground pins: a short stub to a ground symbol each. A tied pin:
    // out, and along to the wire of the pin it's tied to.
    for (const p of b.pins) {
      if (p.dim) continue;
      if (p.ground) {
        out.wires.push({ net: p.net ?? 'GND', path: `M${p.x},${p.y} V${p.y + 8}`, thin: true });
        out.grounds.push({ x: p.x, y: p.y + 8 });
      } else if (p.tied && p.net) {
        const to = tiedTo(b, p);
        if (!to) continue;
        if (p.side === 'left' || p.side === 'right') {
          const x = p.x + (p.side === 'right' ? 10 : -10);
          out.wires.push({ net: p.net, path: `M${p.x},${p.y} H${x} V${to.y}` });
          out.dots.push({ x, y: to.y });
        } else {
          const y = p.y + (p.side === 'bottom' ? 10 : -10);
          out.wires.push({ net: p.net, path: `M${p.x},${p.y} V${y} H${to.x}` });
          out.dots.push({ x: to.x, y });
        }
      }
    }
  }
  // The far end of a passive hanging from a net: a ground symbol, or
  // the name of the net it goes to (a pull-up's rail).
  const farEnd = (p: PlacedPassive, end: End) => {
    const e = p.ends[1];
    if (end.ground) {
      out.wires.push({ net: 'GND', path: `M${e.x},${e.y} V${e.y + 6}`, thin: true });
      out.grounds.push({ x: e.x, y: e.y + 6 });
    } else {
      out.wires.push({ net: end.text, path: `M${e.x},${e.y} V${e.y + 6}`, thin: true });
      out.flags.push({ x: e.x, y: e.y + 6, text: end.text });
    }
  };
  // Passives, with their far end below (or a pull-up's rail above).
  for (const q of built.passives) {
    const n = placed.get(q.node.id)!;
    const p = placePassive(q.passive, at(n.x ?? 0), at(n.y ?? 0), q.size, q.horizontal, q.first);
    if (q.flag) {
      // Its net's name above it, as a schematic's power port or net label.
      const e = p.ends[0];
      out.wires.push({ net: q.flag, path: `M${e.x},${e.y} V${e.y - 6}`, thin: true });
      out.flags.push({ x: e.x, y: e.y - 6, text: q.flag, up: true });
    }
    if (q.bridge) {
      // The symbol between the leads from its ports.
      const px = q.bridge === 'EAST' ? p.x + p.w : p.x;
      p.ends = [
        { x: p.ends[0].x, y: p.y + BRIDGE_IN },
        { x: p.ends[1].x, y: p.y + p.h - BRIDGE_IN },
      ];
      const other = q.passive.nets.find((n) => n !== q.first) ?? q.first;
      p.ends.forEach((e, i) => out.wires.push({ net: i === 0 ? q.first : other, path: `M${px},${e.y} H${e.x}`, thin: true }));
    }
    out.passives.push(p);
    if (q.end) farEnd(p, q.end);
  }
  // Passives under a bottom pin, each on its own drop to the pin.
  for (const u of built.below) {
    const p = placePassive(u.passive, at(u.pin.x - u.size.axis), at(u.pin.y + DROP), u.size, false, u.passive.nets[0]);
    out.passives.push(p);
    out.wires.push({ net: u.passive.nets[0], path: `M${u.pin.x},${u.pin.y} V${p.y}`, thin: true });
    farEnd(p, u.end);
  }
  // Stubs: straight out of their pin, named at the end.
  for (const st of built.stubs) {
    const [boxId, idx] = [st.port.split('/')[0], Number(st.port.split('/')[1])];
    const p = built.boxes.get(boxId)!.pins[idx];
    if (st.side === 'top') {
      // Up from a supply pin, its name above.
      out.wires.push({ net: st.net, path: `M${p.x},${p.y} V${p.y - 24}`, label: { x: p.x, y: p.y - 28, anchor: 'middle', text: st.net } });
      continue;
    }
    if (st.side === 'bottom') {
      out.wires.push({ net: st.net, path: `M${p.x},${p.y} V${p.y + 24}`, label: { x: p.x, y: p.y + 40, anchor: 'middle', text: st.net } });
      continue;
    }
    const dir = st.side === 'right' ? 1 : -1;
    const len = st.net.length * CHAR + 16 + (st.arrow ? 12 : 0);
    const end = p.x + dir * len;
    out.wires.push({ net: st.net, path: `M${p.x},${p.y} H${end}`, arrow: st.arrow || undefined, label: { x: end - dir * (st.arrow ? 12 : 2), y: p.y - 5, anchor: dir > 0 ? 'end' : 'start', text: st.net } });
  }
  // Wires, with their junctions; a net's name above the longest level
  // piece of the wire that carries it (ELK would give each label a layer
  // of its own, which spreads the drawing).
  for (const e of r.edges ?? []) {
    if (e.id.startsWith('g:')) continue; // a ghost edge, for placement only
    const net = e.id.slice(2, e.id.lastIndexOf(':'));
    (e.sections ?? []).forEach((s, k) => {
      const pts = [s.startPoint, ...(s.bendPoints ?? []), s.endPoint].map((p) => ({ x: at(p.x), y: at(p.y) }));
      const wire: Wire = { net, path: pts.map((p, i) => `${i ? 'L' : 'M'}${p.x},${p.y}`).join(' ') };
      out.wires.push(wire);
      if (k > 0 || !built.named.has(e.id)) return;
      // On the first level piece long enough for it; a wire too short
      // for its name everywhere goes unnamed.
      const level = pts.slice(1).map((p, i) => ({ a: pts[i], z: p })).filter(({ a, z }) => a.y === z.y);
      const fits = level.find(({ a, z }) => Math.abs(z.x - a.x) >= net.length * CHAR + 12);
      if (fits) wire.label = { x: at((fits.a.x + fits.z.x) / 2), y: fits.a.y - 5, anchor: 'middle', text: net };
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
