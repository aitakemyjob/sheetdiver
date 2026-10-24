import { describe, expect, it } from 'vitest';
import { THREE_TERMINAL, TWO_TERMINAL, twoTerminal, twoTerminalLeads, twoTerminalTransform } from './symbols';

/** The points a path of M, L, H, V, A and Z commands visits (arcs by their end points). */
function points(path: string): { x: number; y: number }[] {
  const out: { x: number; y: number }[] = [];
  let x = 0;
  let y = 0;
  for (const [, cmd, args] of path.matchAll(/([MLHVAZ])([^MLHVAZ]*)/g)) {
    const n = args
      .trim()
      .split(/[\s,]+/)
      .filter(Boolean)
      .map(Number);
    if (cmd === 'M' || cmd === 'L') [x, y] = [n[0], n[1]];
    if (cmd === 'H') x = n[0];
    if (cmd === 'V') y = n[0];
    if (cmd === 'A') [x, y] = [n[5], n[6]];
    if (cmd !== 'Z') out.push({ x, y });
  }
  return out;
}

describe('the two-terminal symbols', () => {
  it.each(Object.entries(TWO_TERMINAL))('%s runs from its first end to its second along the axis, within its width', (shape, sym) => {
    const pts = points(sym.path + ' ' + (sym.fill ?? ''));
    expect(pts[0]).toEqual({ x: 0, y: -sym.len / 2 });
    expect(
      pts.some((p) => p.x === 0 && p.y === sym.len / 2),
      `${shape} reaches its second end`,
    ).toBe(true);
    for (const p of pts) {
      expect(Math.abs(p.x), `${shape} ${p.x},${p.y}`).toBeLessThanOrEqual(sym.width + 1);
      expect(Math.abs(p.y)).toBeLessThanOrEqual(sym.len / 2);
    }
  });

  it('names the terminals the backend uses', () => {
    expect(TWO_TERMINAL.diode.terminals).toEqual(['anode', 'cathode']);
    expect(TWO_TERMINAL.tvs.terminals).toEqual(['cathode', 'anode']);
    expect(TWO_TERMINAL.battery.terminals).toEqual(['+', '-']);
    expect(twoTerminal('no-such-shape')).toBe(TWO_TERMINAL.resistor);
  });

  it('is laid from one end to the other in any direction', () => {
    expect(twoTerminalTransform({ x: 10, y: 0 }, { x: 10, y: 40 })).toBe('translate(10,20) rotate(0)');
    expect(twoTerminalTransform({ x: 0, y: 10 }, { x: 40, y: 10 })).toBe('translate(20,10) rotate(-90)');
    expect(twoTerminalTransform({ x: 40, y: 10 }, { x: 0, y: 10 })).toBe('translate(20,10) rotate(90)');
    expect(twoTerminalTransform({ x: 10, y: 40 }, { x: 10, y: 0 })).toBe('translate(10,20) rotate(-180)');
  });

  it('gets leads out to ends further apart than it is long, and none otherwise', () => {
    expect(twoTerminalLeads(TWO_TERMINAL.resistor, { x: 0, y: 0 }, { x: 0, y: 36 })).toBe('');
    expect(twoTerminalLeads(TWO_TERMINAL.resistor, { x: 5, y: 0 }, { x: 5, y: 60 })).toBe('M5,0 L5,12 M5,48 L5,60');
    expect(twoTerminalLeads(TWO_TERMINAL.resistor, { x: 0, y: 7 }, { x: 50, y: 7 })).toBe('M0,7 L7,7 M43,7 L50,7');
  });
});

describe('the transistor symbols', () => {
  it.each(Object.entries(THREE_TERMINAL))('%s has its terminals on the border of its box, where its leads end', (shape, sym) => {
    const pts = points(sym.path);
    for (const [name, t] of Object.entries(sym.terminals)) {
      const onBorder = t.x === 0 || t.x === sym.w || t.y === 0 || t.y === sym.h;
      expect(onBorder, `${shape} ${name}`).toBe(true);
      expect(
        pts.some((p) => p.x === t.x && p.y === t.y),
        `${shape} ${name} lead`,
      ).toBe(true);
    }
    for (const p of points(sym.path + ' ' + (sym.fill ?? ''))) {
      expect(p.x).toBeGreaterThanOrEqual(0);
      expect(p.x).toBeLessThanOrEqual(sym.w);
      expect(p.y).toBeGreaterThanOrEqual(0);
      expect(p.y).toBeLessThanOrEqual(sym.h);
    }
  });

  it('names the terminals the backend uses', () => {
    expect(Object.keys(THREE_TERMINAL['mosfet-n'].terminals)).toEqual(['gate', 'drain', 'source']);
    expect(Object.keys(THREE_TERMINAL.pnp.terminals)).toEqual(['base', 'collector', 'emitter']);
  });
});
