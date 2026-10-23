// Which of a net's ends are wired to which, which get a named stub, and
// which wire carries the net's name: decided per net by the first rule
// that applies, on facts about the net alone. Nothing here knows ELK or
// the drawing; blockDiagram.ts turns a plan into edges and stubs.

/** The side of its node an end is on, which says which way the net flows. */
export type Side = 'WEST' | 'EAST' | 'NORTH' | 'SOUTH';

/** One end of a net: a part's pin, a passive's end, or a subgroup's port. */
export interface NetEnd {
  /** The port's id, unique on the drawing. */
  id: string;
  /** The node the port is on. */
  owner: string;
  kind: 'pin' | 'passive' | 'unit';
  side: Side;
  /** A series passive's end, which carries the net on to another. */
  series?: boolean;
  /** A pin's part's column in the flow. */
  column?: number;
  /** The node a passive or subgroup is placed by (its part). */
  placedBy?: string;
  /** The node a passive supports. */
  supports?: string;
  /** The border a subgroup's ports are on. */
  facing?: 'WEST' | 'EAST';
}

/** A net as the rules see it. */
export interface NetContext {
  name: string;
  power: boolean;
  /** The net also runs in other groups. */
  offboard: boolean;
  /** The net leaves the board. */
  out: boolean;
  /** Its stub off the board ends in an arrow. */
  arrow: boolean;
  ends: NetEnd[];
}

/** A short wire out of an end, the net's name at its tip. */
export interface Stub {
  end: string;
  arrow: boolean;
}

/** A wire from one end to another; `named` puts the net's name on it. */
export interface Edge {
  from: string;
  to: string;
  named: boolean;
}

/** What to draw for a net. */
export interface WiringPlan {
  rule: string;
  stubs: Stub[];
  edges: Edge[];
}

/** What the rules decide on, derived once from a net's context. */
export interface NetFacts {
  ctx: NetContext;
  pins: NetEnd[];
  /** The end that drives the net: a passive's right end, a right-side pin, else a bottom pin. */
  driver?: NetEnd;
  /** A series passive carries the net on. */
  series: boolean;
  /** What hangs off the net: passives and subgroups, series ends aside. */
  hung: NetEnd[];
  /** The pin the net is wired at: the driver, else the pin of the part a hanging passive supports, else the first. */
  home?: NetEnd;
  /** The parts with a pin on the net. */
  boxes: Set<string>;
}

/** Derives the facts a net's rules decide on. */
export function facts(ctx: NetContext): NetFacts {
  const pins = ctx.ends.filter((e) => e.kind === 'pin');
  const fromPassive = ctx.ends.find((e) => e.kind === 'passive' && e.side === 'EAST');
  const driver = fromPassive ?? pins.find((e) => e.side === 'EAST') ?? pins.find((e) => e.side === 'SOUTH');
  const series = ctx.ends.some((e) => e.series);
  const hung = ctx.ends.filter((e) => e.kind !== 'pin' && !e.series);
  const serves = hung.map((e) => e.supports).find(Boolean);
  const home = driver ?? pins.find((e) => e.owner === serves) ?? pins[0];
  return { ctx, pins, driver, series, hung, home, boxes: new Set(pins.map((e) => e.owner)) };
}

/** A rule for wiring a net: the first whose `applies` holds plans it. */
export interface WiringRule {
  name: string;
  applies(f: NetFacts): boolean;
  plan(f: NetFacts): WiringPlan;
}

/**
 * A rail nothing here drives isn't wired between its parts, as on a
 * schematic (between just two parts with nothing hanging off it, one
 * wire does). Each pin wires what hangs off it by its part, the first
 * wire named; a pin with nothing gets a stub with the name. What hangs
 * by a part not on the rail here goes at the home pin. A wire runs
 * towards a pin on its part's left (or a subgroup facing right), so what
 * hangs off it sits on that side.
 */
export const undrivenRail: WiringRule = {
  name: 'undriven rail',
  applies: (f) => !f.driver && !f.series && f.ctx.power && f.pins.length > 0 && !(f.pins.length === 2 && f.hung.length === 0),
  plan(f) {
    const plan: WiringPlan = { rule: this.name, stubs: [], edges: [] };
    const rest = f.hung.filter((h) => !f.pins.some((e) => h.placedBy === e.owner));
    for (const pin of f.pins) {
      const mine = [...f.hung.filter((h) => h.placedBy === pin.owner), ...(pin === f.home ? rest : [])];
      if (mine.length === 0) {
        if (pin.side !== 'SOUTH') plan.stubs.push({ end: pin.id, arrow: false });
        continue;
      }
      mine.forEach((h, i) => {
        const before = h.kind === 'unit' ? h.facing === 'EAST' : pin.side === 'WEST';
        plan.edges.push({ from: before ? h.id : pin.id, to: before ? pin.id : h.id, named: i === 0 });
      });
    }
    return plan;
  },
};

/**
 * A signal reaching three parts, or two more than a column apart (a long
 * wire round everything between), is a net label at each pin but the one
 * its passives are wired at.
 */
export const farSignal: WiringRule = {
  name: 'far signal',
  applies(f) {
    if (f.series) return false;
    const boxes = [...f.boxes];
    const column = (owner: string) => f.pins.find((e) => e.owner === owner)?.column ?? 0;
    return boxes.length >= 3 || (boxes.length === 2 && Math.abs(column(boxes[0]) - column(boxes[1])) >= 2);
  },
  plan(f) {
    return wireTail(this.name, f, f.hung.length > 0 && f.home ? [f.home] : []);
  },
};

/** Everything else is wired at the pins, from the end that drives the net. */
export const wiredNet: WiringRule = {
  name: 'wired net',
  applies: () => true,
  plan(f) {
    return wireTail(this.name, f, f.pins);
  },
};

/**
 * Wires the given pins, every hanging passive and every series end
 * together: edges from the end that drives the net to each other end,
 * the first between two parts (else the first at all) carrying the name
 * unless a stub already does; with nothing driving it, each passive
 * comes before the pin it feeds (a pull-up left of an input), not after
 * it at the end of a wire round the part. A pin not wired gets a named
 * stub; a pin alone on its net gets one too, with an arrow when the net
 * leaves the board as an output; an output's driver keeps its arrow
 * stub beside its wires.
 */
function wireTail(rule: string, f: NetFacts, wiredPins: NetEnd[]): WiringPlan {
  const plan: WiringPlan = { rule, stubs: [], edges: [] };
  const wired = [...wiredPins, ...f.hung, ...f.ctx.ends.filter((e) => e.series)];
  const leaves = f.ctx.offboard || (f.ctx.out && !f.series);
  for (const e of f.pins) if (!wiredPins.includes(e) && e.side !== 'SOUTH') plan.stubs.push({ end: e.id, arrow: false });
  if (wired.length === 1 && wired[0].kind === 'pin' && wired[0].side !== 'SOUTH') plan.stubs.push({ end: wired[0].id, arrow: leaves && f.ctx.arrow });
  if (wired.length < 2) return plan;
  if (leaves && f.ctx.arrow && f.driver && f.driver.kind === 'pin' && f.driver.side !== 'SOUTH') plan.stubs.push({ end: f.driver.id, arrow: true });
  const source = f.driver ?? wired.find((e) => e.kind !== 'pin') ?? wired[0];
  const sinks = wired.filter((e) => e !== source);
  const between = sinks.find((e) => e.kind === 'pin' && source.kind === 'pin');
  const feeds = !f.driver ? wiredPins[0] : undefined;
  let named = plan.stubs.length > 0;
  sinks.forEach((e, i) => {
    const [from, to] = feeds && e.kind !== 'pin' ? [e, feeds] : [source, e];
    const carries = !named && (e === between || (!between && i === 0));
    named ||= carries;
    plan.edges.push({ from: from.id, to: to.id, named: carries });
  });
  return plan;
}

/** The rules, in the order they're tried. */
export const WIRING_RULES: readonly WiringRule[] = [undrivenRail, farSignal, wiredNet];

/** Plans a net's wiring: the first rule that applies decides. */
export function wire(ctx: NetContext, rules: readonly WiringRule[] = WIRING_RULES): WiringPlan {
  if (ctx.ends.length === 0) return { rule: 'nothing', stubs: [], edges: [] };
  const f = facts(ctx);
  const rule = rules.find((r) => r.applies(f)) ?? wiredNet;
  return rule.plan(f);
}
