import { describe, expect, it } from 'vitest';
import { placeFrames, type Frame } from './placement';

const f = (name: string, w: number, h: number, below?: string): Frame => ({ name, w, h, below });
const at = (frames: Frame[]) => Object.fromEntries(placeFrames(frames, 10).map((p) => [p.name, [p.x, p.y, p.column]]));

describe('placing group frames', () => {
  it('puts frames left to right in the order given when none is below another', () => {
    expect(at([f('USB', 100, 50), f('Power', 200, 80), f('MCU', 300, 120)])).toEqual({ USB: [0, 0, 0], Power: [110, 0, 1], MCU: [320, 0, 2] });
  });

  it("stacks a frame under the one it's below, its column as wide as its widest frame", () => {
    expect(at([f('Power', 200, 80), f('MCU', 300, 120), f('Debug', 100, 40), f('Clock', 400, 60, 'MCU')])).toEqual({
      Power: [0, 0, 0],
      MCU: [210, 0, 1],
      Clock: [210, 130, 1],
      // Debug's column starts after MCU's column, which Clock widened.
      Debug: [620, 0, 2],
    });
  });

  it('stacks a chain, and several under one frame in order, depth first', () => {
    const p = at([f('MCU', 100, 100), f('Clock', 100, 50, 'MCU'), f('Debug', 100, 30, 'Clock'), f('LED', 100, 20, 'MCU')]);
    expect(p).toEqual({ MCU: [0, 0, 0], Clock: [0, 110, 0], Debug: [0, 170, 0], LED: [0, 210, 0] });
  });

  it('stacks under a frame that comes later in the order, its column where that frame starts one', () => {
    expect(at([f('Clock', 100, 50, 'MCU'), f('Power', 100, 50), f('MCU', 100, 100)])).toEqual({ Power: [0, 0, 0], MCU: [110, 0, 1], Clock: [110, 110, 1] });
  });

  it('gives a frame below one not drawn, or in a loop, a column of its own; one below a loop goes with it', () => {
    expect(at([f('A', 100, 10, 'Nope'), f('B', 100, 10)])).toEqual({ A: [0, 0, 0], B: [110, 0, 1] });
    const p = placeFrames([f('A', 100, 10, 'B'), f('B', 100, 10, 'A'), f('C', 100, 10, 'A')], 10);
    expect(p.map((x) => x.column)).toEqual([0, 1, 0]);
  });

  it('matches names whatever their case', () => {
    expect(at([f('MCU', 100, 100), f('Clock', 100, 50, 'mcu')]).Clock).toEqual([0, 110, 0]);
  });
});
