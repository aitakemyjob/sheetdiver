// How a passive is drawn: across two pins of one part, in series between
// two nets that reach different parts, or hanging off one net with its
// far end shown. Decided by the first rule that applies, on facts about
// the passive and which parts its nets reach; nothing here knows ELK.

/** What's shown at a hanging passive's far end: a ground symbol, or the net it goes to. */
export type FarEnd = { ground: true } | { ground: false; text: string };

/** The facts a passive's rules decide on. */
export interface PassiveFacts {
  nets: string[];
  mount: string;
  /** The node it supports, if set. */
  supports?: string;
  /** The parts (and subgroups) a net reaches here. */
  reaching(net: string): Set<string>;
  power(net: string): boolean;
  /** Something drives the net: a right-side pin or a passive's right end is on it. */
  driven(net: string): boolean;
  /** The part it bridges has both its nets on pins that can be bridged. */
  bridged: boolean;
}

/** How a passive is drawn. */
export type Hanging =
  | { kind: 'bridge' }
  | { kind: 'series'; upstream: string; downstream: string }
  | { kind: 'hung'; from: string; far: FarEnd };

/** A rule for drawing a passive: the first whose `applies` holds decides. */
export interface HangingRule {
  name: string;
  applies(f: PassiveFacts): boolean;
  hang(f: PassiveFacts): Hanging;
}

/** The nets of a passive that reach a part here. */
const reaching = (f: PassiveFacts) => f.nets.filter((n) => f.reaching(n).size > 0);

/** Across two pins of one part, beside them. */
export const bridge: HangingRule = {
  name: 'bridge',
  applies: (f) => f.mount === 'bridge' && f.nets.length === 2 && f.bridged,
  hang: () => ({ kind: 'bridge' }),
};

/**
 * A pull-up (or a diode to a signal) hangs from its signal, its rail
 * named at the far end, when the signal reaches a part here.
 */
export const pullUp: HangingRule = {
  name: 'pull-up',
  applies(f) {
    if (f.nets.length !== 2 || f.power(f.nets[0]) === f.power(f.nets[1])) return false;
    return f.reaching(f.nets.find((n) => !f.power(n))!).size > 0;
  },
  hang(f) {
    const signal = f.nets.find((n) => !f.power(n))!;
    return { kind: 'hung', from: signal, far: { ground: false, text: f.nets.find((n) => n !== signal)! } };
  },
};

/**
 * In series between two nets that reach different parts: upstream is
 * the net something drives, else the one not at the part it serves.
 */
export const series: HangingRule = {
  name: 'series',
  applies(f) {
    const nets = reaching(f);
    return nets.length === 2 && !setsEqual(f.reaching(nets[0]), f.reaching(nets[1]));
  },
  hang(f) {
    const [a, b] = reaching(f);
    const flipped = (f.driven(b) && !f.driven(a)) || (!f.driven(a) && f.reaching(a).has(`b:${f.supports}`));
    return flipped ? { kind: 'series', upstream: b, downstream: a } : { kind: 'series', upstream: a, downstream: b };
  },
};

/**
 * Hanging off the first net that reaches a part here, its far end named
 * (the other net, a resistor feeding an LED off 3V3), else to ground.
 */
export const hung: HangingRule = {
  name: 'hung',
  applies: (f) => reaching(f).length > 0,
  hang(f) {
    const from = reaching(f)[0];
    const other = f.nets.find((n) => n !== from);
    return { kind: 'hung', from, far: other ? { ground: false, text: other } : { ground: true } };
  },
};

/** The rules, in the order they're tried. */
export const HANGING_RULES: readonly HangingRule[] = [bridge, pullUp, series, hung];

/** How a passive is drawn, or nothing when none of its nets reaches a part here. */
export function hang(f: PassiveFacts, rules: readonly HangingRule[] = HANGING_RULES): Hanging | undefined {
  return rules.find((r) => r.applies(f))?.hang(f);
}

/** Whether two sets hold the same members. */
export function setsEqual<T>(a: Set<T>, b: Set<T>): boolean {
  return a.size === b.size && [...a].every((x) => b.has(x));
}
