import { describe, expect, it } from 'vitest';
import type { Diagram, DiagramPart, DiagramPassive } from '../types';
import { candidates, errorText, LayoutError, MAX_TRIES, only, salvage, skippedText, titleOf, without } from './salvage';

const part = (id: string, title: string): DiagramPart => ({ id, title, kind: 'ic', shape: 'ic', column: 0, pins: [] });
const passive = (id: string, label: string, by: string, nets: string[]): DiagramPassive =>
  ({ id, label, kind: 'resistor', shape: 'resistor', ends: ['1', '2'], mount: 'below', part: by, nets }) as DiagramPassive;

const board: Diagram = {
  parts: [part('part:1', 'U1 BUCK'), part('part:2/Q4', 'Q4 FET')],
  // R1 hangs by Q4, R2 is chained after R1, C1 is U1's.
  passives: [passive('block:R1', 'R1 10k', 'part:2/Q4', ['G4']), passive('block:R2', 'R2 1k', 'block:R1', ['G4']), passive('block:C1', 'C1 1µF', 'part:1', ['VIN'])],
  nets: [
    { name: 'VIN', kind: 'power', parts: ['part:1'], wire: 'out' },
    { name: 'G4', kind: 'signal', parts: ['part:1', 'part:2/Q4'], wire: 'between' },
  ],
  groups: [{ name: 'Power', parts: ['part:1', 'part:2/Q4'] }],
  links: [],
  subgroups: [{ kind: 'rail', net: 'G4', members: ['part:2/Q4', 'block:R1'] }],
};

describe('leaving a part off a diagram', () => {
  it('takes the part, the passives drawn by it and every mention of them', () => {
    const { diagram, passives } = without(board, ['part:2/Q4']);
    expect(diagram.parts.map((p) => p.id)).toEqual(['part:1']);
    expect(diagram.passives.map((q) => q.id)).toEqual(['block:C1']);
    expect(passives.get('part:2/Q4')).toEqual(['R1 10k', 'R2 1k']);
    expect(diagram.nets.find((n) => n.name === 'G4')?.parts).toEqual(['part:1']);
    expect(diagram.groups[0].parts).toEqual(['part:1']);
    // A subgroup left with nobody in it goes.
    expect(diagram.subgroups).toEqual([]);
    // The diagram it came from is untouched.
    expect(board.parts).toHaveLength(2);
  });

  it('keeps one part alone, with its passives', () => {
    const d = only(board, 'part:2/Q4');
    expect(d.parts.map((p) => p.id)).toEqual(['part:2/Q4']);
    expect(d.passives.map((q) => q.id)).toEqual(['block:R1', 'block:R2']);
    expect(d.nets.find((n) => n.name === 'VIN')?.parts).toEqual([]);
  });

  it('tries parts first, then passives, at most MAX_TRIES', () => {
    expect(candidates(board)).toEqual(['part:1', 'part:2/Q4', 'block:R1', 'block:R2', 'block:C1']);
    const big: Diagram = { ...board, parts: Array.from({ length: 50 }, (_, i) => part(`part:${i}`, `U${i}`)) };
    expect(candidates(big)).toHaveLength(MAX_TRIES);
  });

  it('names what it left off, and why', () => {
    expect(titleOf(board, 'part:2/Q4')).toBe('Q4 FET');
    expect(titleOf(board, 'block:R1')).toBe('R1 10k');
    expect(titleOf(board, 'part:9')).toBe('part:9');
    expect(skippedText({ node: 'part:2/Q4', title: 'Q4 FET', error: 'boom', with: ['R1 10k'] })).toBe("Q4 FET (with R1 10k) couldn't be drawn and is left off: boom");
    expect(errorText(new Error('boom'))).toBe('boom');
    expect(errorText('plain')).toBe('plain');
    expect(new LayoutError('boom', 3).message).toBe('boom (it still failed with each of 3 parts and passives left off in turn)');
    expect(new LayoutError('boom', 0).message).toBe('boom');
  });

  // A layout that fails on any diagram with one of the given parts in it.
  const failsWith =
    (...bad: string[]) =>
    async (d: Diagram) => {
      const hit = d.parts.find((p) => bad.includes(p.id));
      if (hit) throw new Error(`trips on ${hit.id}`);
      return d.parts.map((p) => p.id);
    };

  it('leaves off every part that fails alone, at once', async () => {
    const r = await salvage(board, 'boom', failsWith('part:2/Q4'));
    expect(r.drawing).toEqual(['part:1']);
    expect(r.skipped).toEqual([{ node: 'part:2/Q4', title: 'Q4 FET', error: 'boom', with: ['R1 10k', 'R2 1k'] }]);
    const three: Diagram = { ...board, parts: [...board.parts, part('part:3/Q5', 'Q5 FET')] };
    const both = await salvage(three, 'boom', failsWith('part:2/Q4', 'part:3/Q5'));
    expect(both.skipped.map((x) => x.node)).toEqual(['part:2/Q4', 'part:3/Q5']);
  });

  it('else leaves off one part (with its passives) at a time', async () => {
    // Laid out alone every part is fine; together, with C1, they fail.
    const layout = async (d: Diagram) => {
      if (d.parts.length > 1 && d.passives.some((q) => q.id === 'block:C1')) throw new Error('clash');
      return d.passives.map((q) => q.id);
    };
    const r = await salvage(board, 'clash', layout);
    expect(r.skipped).toEqual([{ node: 'part:1', title: 'U1 BUCK', error: 'clash', with: ['C1 1µF'] }]);
    expect(r.drawing).toEqual(['block:R1', 'block:R2']);
  });

  it("gives up when nothing helps, or every part fails alone", async () => {
    await expect(salvage(board, 'boom', failsWith('part:1', 'part:2/Q4'))).rejects.toThrow('boom (U1 BUCK, Q4 FET each fail even drawn alone)');
    const clash = async (d: Diagram) => {
      if (d.parts.length > 1) throw new Error('clash');
      return [];
    };
    const three: Diagram = { ...board, parts: [...board.parts, part('part:3', 'U3 LDO')], passives: [] };
    await expect(salvage(three, 'clash', clash)).rejects.toThrow('clash (it still failed with each of 3 parts and passives left off in turn)');
    expect(new LayoutError('boom', 1, ['Q4 FET']).message).toBe('boom (Q4 FET fails even drawn alone)');
  });
});
