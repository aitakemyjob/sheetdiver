// The standard schematic symbols, as SVG path geometry of our own (no
// symbol library): the two-terminals (ANSI style: a zigzag resistor), and
// the transistors. Which symbol a part gets, and which of its terminals is
// at which net, is the backend's call (internal/core/symbol.go); this
// file only knows how each looks.

import type { PartShape, PassiveShape } from './types';

/** A point. */
type Pt = { x: number; y: number };

/**
 * A two-terminal symbol drawn along the y axis, its first terminal at
 * (0, -len/2) and its second at (0, len/2), leads included. `path` is
 * stroked, `fill` filled with the stroke colour (a diode's triangle).
 */
export interface TwoTerminal {
  len: number;
  /** How far it reaches either side of the axis. */
  width: number;
  path: string;
  fill?: string;
  /** Its terminals, first and second, as the backend names them. */
  terminals: [string, string];
}

const diode = (bar: string, extra = ''): TwoTerminal => ({ len: 36, width: 14, path: `M0,-18 V-7 M0,7 V18 ${bar}${extra}`, fill: 'M-7,-7 H7 L0,7 Z', terminals: ['anode', 'cathode'] });

/** The two-terminal symbols by shape. */
export const TWO_TERMINAL: Record<PassiveShape, TwoTerminal> = {
  resistor: { len: 36, width: 5, path: 'M0,-18 V-15 L5,-12.5 L-5,-7.5 L5,-2.5 L-5,2.5 L5,7.5 L-5,12.5 L0,15 V18', terminals: ['1', '2'] },
  capacitor: { len: 36, width: 8, path: 'M0,-18 V-3 M-8,-3 H8 M-8,3 H8 M0,3 V18', terminals: ['1', '2'] },
  'capacitor-polarised': { len: 36, width: 15, path: 'M0,-18 V-3 M-8,-3 H8 M-8,8 A12,12 0 0 1 8,8 M0,4 V18 M-15,-13 H-9 M-12,-16 V-10', terminals: ['+', '-'] },
  inductor: { len: 36, width: 4, path: 'M0,-18 V-16 A4,4 0 0 1 0,-8 A4,4 0 0 1 0,0 A4,4 0 0 1 0,8 A4,4 0 0 1 0,16 V18', terminals: ['1', '2'] },
  diode: diode('M-7,7 H7'),
  schottky: diode('M-7,10 V7 H7 V4'),
  zener: diode('M-9,4 L-7,7 H7 L9,10'),
  led: { ...diode('M-7,7 H7', ' M4,-1 L11,-8 M11,-8 H7 M11,-8 V-4 M7,4 L14,-3 M14,-3 H10 M14,-3 V1'), width: 14 },
  tvs: { len: 36, width: 7, path: 'M0,-18 V-12 M0,12 V18 M-7,0 H7', fill: 'M-7,-12 H7 L0,0 Z M-7,12 H7 L0,0 Z', terminals: ['cathode', 'anode'] },
  fuse: { len: 36, width: 5, path: 'M0,-18 V18 M-5,-12 H5 V12 H-5 Z', terminals: ['1', '2'] },
  ferrite: { len: 36, width: 4, path: 'M0,-18 V-12 M0,12 V18 M-4,-12 H4 V12 H-4 Z M-4,-5 L4,-9 M-4,2 L4,-2 M-4,9 L4,5', terminals: ['1', '2'] },
  crystal: { len: 36, width: 8, path: 'M0,-18 V-8 M-8,-8 H8 M-8,8 H8 M0,8 V18 M-6,-4 H6 V4 H-6 Z', terminals: ['1', '2'] },
  battery: { len: 36, width: 16, path: 'M0,-18 V-5 M-10,-5 H10 M-4,-1 H4 M-10,3 H10 M-4,7 H4 M0,7 V18 M-16,-14 H-10 M-13,-17 V-11', terminals: ['+', '-'] },
  switch: { len: 36, width: 9, path: 'M0,-18 V-11 M-2,-9 A2,2 0 1 0 2,-9 A2,2 0 1 0 -2,-9 M-2,9 A2,2 0 1 0 2,9 A2,2 0 1 0 -2,9 M0,11 V18 M1,-8 L9,6', terminals: ['1', '2'] },
};

/** The symbol of a shape the backend names, a resistor for one it doesn't. */
export function twoTerminal(shape: string): TwoTerminal {
  return TWO_TERMINAL[shape as PassiveShape] ?? TWO_TERMINAL.resistor;
}

/**
 * The transform that lays a two-terminal symbol from its first end at
 * `a` to its second at `z`: moved to their midpoint and turned to their
 * direction (the symbol is drawn down the y axis, so straight down is no
 * turn and left to right a quarter turn anticlockwise).
 */
export function twoTerminalTransform(a: Pt, z: Pt): string {
  const deg = (Math.atan2(z.y - a.y, z.x - a.x) * 180) / Math.PI - 90;
  return `translate(${(a.x + z.x) / 2},${(a.y + z.y) / 2}) rotate(${Math.round(deg * 100) / 100})`;
}

/** The leads from a symbol's ends out to `a` and `z`, when they're further apart than the symbol is long. */
export function twoTerminalLeads(sym: TwoTerminal, a: Pt, z: Pt): string {
  const d = Math.hypot(z.x - a.x, z.y - a.y);
  if (d <= sym.len) return '';
  const u = { x: (z.x - a.x) / d, y: (z.y - a.y) / d };
  const over = (d - sym.len) / 2;
  const p = (from: Pt, k: number) => `${Math.round((from.x + u.x * k) * 100) / 100},${Math.round((from.y + u.y * k) * 100) / 100}`;
  return `M${a.x},${a.y} L${p(a, over)} M${p(z, -over)} L${z.x},${z.y}`;
}

/**
 * A transistor symbol in a box of w by h, its terminals on the box's
 * border where the pin lines end: the gate or base on the left, the
 * drain or collector on top, the source or emitter at the bottom.
 */
export interface ThreeTerminal {
  w: number;
  h: number;
  path: string;
  fill?: string;
  terminals: Record<string, Pt>;
}

const mosfet = (p: boolean): ThreeTerminal => ({
  w: 60,
  h: 60,
  // Gate plate, three channel segments, the drain and source leads to
  // the channel; the arrow on the body connection points in for an N
  // channel and out for a P channel, which also has a bubble on its gate.
  path: `M0,30 H${p ? 10 : 16} ${p ? 'M10,30 A3,3 0 1 0 16,30 A3,3 0 1 0 10,30 ' : ''}M16,18 V42 M22,16 V24 M22,26 V34 M22,36 V44 M30,0 V20 H22 M30,60 V30 H22`,
  fill: p ? 'M30,30 L23,26.5 L23,33.5 Z' : 'M22,30 L29,26.5 L29,33.5 Z',
  terminals: { gate: { x: 0, y: 30 }, drain: { x: 30, y: 0 }, source: { x: 30, y: 60 } },
});

const bjt = (pnp: boolean): ThreeTerminal => ({
  w: 60,
  h: 60,
  // Base lead and bar, collector and emitter leads; the emitter's arrow
  // points out of an NPN and into a PNP.
  path: 'M0,30 H20 M20,20 V40 M30,0 V18 L20,27 M20,33 L30,42 V60',
  fill: pnp ? 'M20,33 L23.2,39.9 L27.2,35.5 Z' : 'M30,42 L22.8,39.5 L26.8,35.1 Z',
  terminals: { base: { x: 0, y: 30 }, collector: { x: 30, y: 0 }, emitter: { x: 30, y: 60 } },
});

/** The transistor symbols by shape. */
export const THREE_TERMINAL: Record<Exclude<PartShape, 'ic'>, ThreeTerminal> = {
  'mosfet-n': mosfet(false),
  'mosfet-p': mosfet(true),
  npn: bjt(false),
  pnp: bjt(true),
};

/** The transistor symbol of a shape, if it is one. */
export function threeTerminal(shape: string): ThreeTerminal | undefined {
  return THREE_TERMINAL[shape as Exclude<PartShape, 'ic'>];
}
