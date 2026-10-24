import { describe, expect, it } from 'vitest';
import { facts, farSignal, undrivenRail, wire, wiredNet, type NetContext, type NetEnd } from './wiring';

/** A net with the given ends; a power rail when `power`. */
const net = (ends: NetEnd[], extra: Partial<NetContext> = {}): NetContext => ({ name: 'N', power: false, offboard: false, out: false, arrow: false, ends, ...extra });
const pin = (owner: string, side: NetEnd['side'], extra: Partial<NetEnd> = {}): NetEnd => ({ id: `${owner}/${side}`, owner, kind: 'pin', side, column: 0, ...extra });
const hung = (id: string, placedBy: string, extra: Partial<NetEnd> = {}): NetEnd => ({ id: `${id}/0`, owner: id, kind: 'passive', side: 'NORTH', placedBy, ...extra });
const unit = (id: string, placedBy: string, facing: 'WEST' | 'EAST'): NetEnd => ({ id: `${id}/0`, owner: id, kind: 'unit', side: facing, placedBy, facing });
const seriesEnd = (id: string, side: 'WEST' | 'EAST'): NetEnd => ({ id: `${id}/${side === 'WEST' ? 0 : 1}`, owner: id, kind: 'passive', side, series: true });

describe('the facts of a net', () => {
  it('finds the driver: a passive right end, else a right-side pin, else a bottom pin', () => {
    expect(facts(net([pin('b:u1', 'WEST'), pin('b:u2', 'SOUTH')])).driver?.id).toBe('b:u2/SOUTH');
    expect(facts(net([pin('b:u1', 'WEST'), pin('b:u2', 'SOUTH'), pin('b:u3', 'EAST')])).driver?.id).toBe('b:u3/EAST');
    expect(facts(net([pin('b:u3', 'EAST'), seriesEnd('q:r1', 'EAST')])).driver?.id).toBe('q:r1/1');
    // A subgroup never drives a net, whichever border its port is on.
    expect(facts(net([pin('b:u1', 'WEST'), unit('u:x', 'b:u1', 'EAST')])).driver).toBeUndefined();
  });

  it('wires a net at the pin of the part a hanging passive supports when nothing drives it', () => {
    const f = facts(net([pin('b:u1', 'WEST'), pin('b:u2', 'WEST'), hung('q:c1', 'b:u2', { supports: 'b:u2' })]));
    expect(f.home?.owner).toBe('b:u2');
    expect(f.hung.map((e) => e.owner)).toEqual(['q:c1']);
    expect([...f.boxes]).toEqual(['b:u1', 'b:u2']);
  });
});

describe('an undriven rail', () => {
  it('applies to a power net nothing drives with a pin on it, but not to two bare pins', () => {
    const rail = (ends: NetEnd[]) => undrivenRail.applies(facts(net(ends, { power: true })));
    expect(rail([pin('b:u1', 'WEST')])).toBe(true);
    expect(rail([pin('b:u1', 'WEST'), pin('b:u2', 'WEST')])).toBe(false);
    expect(rail([pin('b:u1', 'WEST'), pin('b:u2', 'WEST'), hung('q:c1', 'b:u1')])).toBe(true);
    expect(rail([pin('b:u1', 'WEST'), pin('b:u2', 'WEST'), pin('b:u3', 'WEST')])).toBe(true);
    expect(rail([pin('b:u1', 'EAST'), pin('b:u2', 'WEST')])).toBe(false); // driven
    expect(undrivenRail.applies(facts(net([pin('b:u1', 'WEST'), pin('b:u2', 'WEST'), pin('b:u3', 'WEST')])))).toBe(false); // a signal
  });

  it('wires each pin to what hangs off it by its part, the first wire named, and stubs the rest', () => {
    const plan = wire(net([pin('b:u1', 'WEST'), pin('b:u2', 'EAST', { side: 'NORTH' }), pin('b:u3', 'EAST', { side: 'NORTH' }), hung('q:c1', 'b:u1'), hung('q:c2', 'b:u1'), hung('q:c3', 'b:u2')], { power: true }));
    expect(plan.rule).toBe('undriven rail');
    // Towards a pin on the part's left: the capacitors sit on that side.
    expect(plan.edges).toEqual([
      { from: 'q:c1/0', to: 'b:u1/WEST', named: true },
      { from: 'q:c2/0', to: 'b:u1/WEST', named: false },
      { from: 'b:u2/EAST', to: 'q:c3/0', named: true },
    ]);
    expect(plan.stubs).toEqual([{ end: 'b:u3/EAST', arrow: false }]);
  });

  it('puts what hangs by a part not on the rail at the home pin, and runs a wire towards a subgroup facing right', () => {
    const plan = wire(net([pin('b:u1', 'NORTH'), pin('b:u2', 'NORTH'), hung('q:c9', 'b:elsewhere'), unit('u:caps', 'b:u2', 'EAST')], { power: true }));
    expect(plan.edges).toEqual([
      { from: 'b:u1/NORTH', to: 'q:c9/0', named: true },
      { from: 'u:caps/0', to: 'b:u2/NORTH', named: true },
    ]);
    expect(plan.stubs).toEqual([]);
  });

  it('gives a bottom pin with nothing no stub', () => {
    expect(wire(net([pin('b:u1', 'SOUTH'), hung('q:c1', 'b:nobody')], { power: true })).stubs).toEqual([]);
  });
});

describe('a far signal', () => {
  it('applies to a signal reaching three parts, or two more than a column apart, never a series net', () => {
    expect(farSignal.applies(facts(net([pin('b:a', 'EAST'), pin('b:b', 'WEST'), pin('b:c', 'WEST')])))).toBe(true);
    expect(farSignal.applies(facts(net([pin('b:a', 'EAST'), pin('b:b', 'WEST', { column: 2 })])))).toBe(true);
    expect(farSignal.applies(facts(net([pin('b:a', 'EAST'), pin('b:b', 'WEST', { column: 1 })])))).toBe(false);
    expect(farSignal.applies(facts(net([pin('b:a', 'EAST'), pin('b:b', 'WEST', { column: 2 }), seriesEnd('q:r', 'WEST')])))).toBe(false);
  });

  it('labels every pin, wiring only the home pin to what hangs off the net', () => {
    const plan = wire(net([pin('b:a', 'EAST'), pin('b:b', 'WEST'), pin('b:c', 'WEST'), hung('q:c1', 'b:a')]));
    expect(plan.rule).toBe('far signal');
    expect(plan.stubs.map((s) => s.end)).toEqual(['b:b/WEST', 'b:c/WEST']);
    // The stubs name the net, so the wire doesn't.
    expect(plan.edges).toEqual([{ from: 'b:a/EAST', to: 'q:c1/0', named: false }]);
  });

  it('labels every pin when nothing hangs off the net', () => {
    const plan = wire(net([pin('b:a', 'EAST'), pin('b:b', 'WEST'), pin('b:c', 'WEST')]));
    expect(plan.stubs.map((s) => s.end)).toEqual(['b:a/EAST', 'b:b/WEST', 'b:c/WEST']);
    expect(plan.edges).toEqual([]);
  });
});

describe('a wired net', () => {
  it('wires from the driver to each other end, the wire between two parts carrying the name', () => {
    const plan = wire(net([pin('b:u1', 'EAST'), hung('q:c1', 'b:u1'), pin('b:u2', 'WEST')]));
    expect(plan.rule).toBe('wired net');
    // Pins first, then what hangs off the net.
    expect(plan.edges).toEqual([
      { from: 'b:u1/EAST', to: 'b:u2/WEST', named: true },
      { from: 'b:u1/EAST', to: 'q:c1/0', named: false },
    ]);
    expect(plan.stubs).toEqual([]);
  });

  it('puts a passive before the pin it feeds when nothing drives the net', () => {
    const plan = wire(net([pin('b:u1', 'WEST'), hung('q:r1', 'b:u1')]));
    expect(plan.edges).toEqual([{ from: 'q:r1/0', to: 'b:u1/WEST', named: true }]);
  });

  it('gives a pin alone on its net a named stub, with an arrow for an output leaving the board', () => {
    expect(wire(net([pin('b:u1', 'WEST')])).stubs).toEqual([{ end: 'b:u1/WEST', arrow: false }]);
    expect(wire(net([pin('b:u1', 'EAST')], { out: true, arrow: true })).stubs).toEqual([{ end: 'b:u1/EAST', arrow: true }]);
    expect(wire(net([pin('b:u1', 'EAST')], { offboard: true, arrow: true })).stubs).toEqual([{ end: 'b:u1/EAST', arrow: true }]);
    expect(wire(net([pin('b:u1', 'SOUTH')])).stubs).toEqual([]);
  });

  it("keeps an output's arrow stub beside the wires to what hangs off it, which then go unnamed", () => {
    const plan = wire(net([pin('b:u1', 'EAST'), hung('q:c1', 'b:u1')], { out: true, arrow: true }));
    expect(plan.stubs).toEqual([{ end: 'b:u1/EAST', arrow: true }]);
    expect(plan.edges).toEqual([{ from: 'b:u1/EAST', to: 'q:c1/0', named: false }]);
  });

  it('carries a series passive on: from its right end to the next part', () => {
    const plan = wire(net([seriesEnd('q:r5', 'EAST'), pin('b:d1', 'WEST')]));
    expect(plan.edges).toEqual([{ from: 'q:r5/1', to: 'b:d1/WEST', named: true }]);
  });

  it('is the fallback: a net with no ends plans nothing, and the chain never runs out of rules', () => {
    expect(wire(net([]))).toEqual({ rule: 'nothing', stubs: [], edges: [] });
    expect(wire(net([pin('b:u1', 'WEST'), pin('b:u2', 'EAST')]), [])).toMatchObject({ rule: wiredNet.name });
  });
});
