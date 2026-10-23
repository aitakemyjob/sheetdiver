import { describe, expect, it } from 'vitest';
import { bridge, hang, pullUp, series, type PassiveFacts } from './hanging';

/** A passive's facts: which parts each net reaches, which nets are rails and which are driven. */
function facts(nets: string[], reach: Record<string, string[]>, extra: { mount?: string; supports?: string; bridged?: boolean; rails?: string[]; driven?: string[] } = {}): PassiveFacts {
  const { rails = [], driven = [], mount = 'wire', bridged = false, supports } = extra;
  return {
    nets,
    mount,
    supports,
    bridged,
    reaching: (n) => new Set(reach[n] ?? []),
    power: (n) => rails.includes(n),
    driven: (n) => driven.includes(n),
  };
}

describe('how a passive is drawn', () => {
  it('bridges two pins of one part when mounted so and both pins are there', () => {
    expect(hang(facts(['LX1', 'LX2'], { LX1: ['b:u1'], LX2: ['b:u1'] }, { mount: 'bridge', bridged: true }))).toEqual({ kind: 'bridge' });
    // Without both pins it falls through: both nets reach the same part, so it hangs off the first.
    expect(bridge.applies(facts(['LX1', 'LX2'], { LX1: ['b:u1'], LX2: ['b:u1'] }, { mount: 'bridge' }))).toBe(false);
    expect(hang(facts(['LX1', 'LX2'], { LX1: ['b:u1'], LX2: ['b:u1'] }, { mount: 'bridge' }))).toEqual({ kind: 'hung', from: 'LX1', far: { ground: false, text: 'LX2' } });
  });

  it('hangs a pull-up from its signal with the rail named below, when the signal reaches a part', () => {
    expect(hang(facts(['3V3', 'SDA'], { '3V3': ['b:u1'], SDA: ['b:u1'] }, { rails: ['3V3'] }))).toEqual({ kind: 'hung', from: 'SDA', far: { ground: false, text: '3V3' } });
    expect(hang(facts(['SDA', '3V3'], { SDA: ['b:u1'] }, { rails: ['3V3'] }))).toEqual({ kind: 'hung', from: 'SDA', far: { ground: false, text: '3V3' } });
  });

  it('hangs a resistor from its rail when its signal reaches no part: a net of passives only', () => {
    expect(pullUp.applies(facts(['3V3', 'LED_A'], { '3V3': ['b:j2'] }, { rails: ['3V3'] }))).toBe(false);
    expect(hang(facts(['3V3', 'LED_A'], { '3V3': ['b:j2'] }, { rails: ['3V3'] }))).toEqual({ kind: 'hung', from: '3V3', far: { ground: false, text: 'LED_A' } });
  });

  it('puts a passive in series between nets reaching different parts, upstream the driven one', () => {
    expect(hang(facts(['PA5', 'LED'], { PA5: ['b:u2'], LED: ['b:d1'] }, { driven: ['PA5'] }))).toEqual({ kind: 'series', upstream: 'PA5', downstream: 'LED' });
    expect(hang(facts(['LED', 'PA5'], { PA5: ['b:u2'], LED: ['b:d1'] }, { driven: ['PA5'] }))).toEqual({ kind: 'series', upstream: 'PA5', downstream: 'LED' });
    // Nothing driven: upstream is the net not at the part it serves.
    expect(hang(facts(['LED', 'PA5'], { PA5: ['b:u2'], LED: ['b:d1'] }, { supports: 'd1' }))).toEqual({ kind: 'series', upstream: 'PA5', downstream: 'LED' });
    expect(hang(facts(['PA5', 'LED'], { PA5: ['b:u2'], LED: ['b:d1'] }))).toEqual({ kind: 'series', upstream: 'PA5', downstream: 'LED' });
  });

  it('is not series between two nets reaching the same parts, nor between a rail and a signal', () => {
    expect(series.applies(facts(['A', 'B'], { A: ['b:u1'], B: ['b:u1'] }))).toBe(false);
    expect(hang(facts(['3V3', 'EN'], { '3V3': ['b:u1'], EN: ['b:u2'] }, { rails: ['3V3'] }))).toEqual({ kind: 'hung', from: 'EN', far: { ground: false, text: '3V3' } });
  });

  it('hangs a one-net passive to ground, and draws nothing when no net reaches a part', () => {
    expect(hang(facts(['3V3'], { '3V3': ['b:u1'] }))).toEqual({ kind: 'hung', from: '3V3', far: { ground: true } });
    expect(hang(facts(['LED_A'], {}))).toBeUndefined();
  });
});
