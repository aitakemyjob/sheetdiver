import { describe, expect, it } from 'vitest';
import type { BlockDrawing, Box, PlacedPassive, PlacedPin, Wire } from '../blockDiagram';
import type { DiagramGroup, DiagramPart, DiagramPassive } from '../types';
import { crossingWires, lint, overlappingParts, pathPoints, wireRoundPart, wireThroughPart, type LayoutCheck } from './lint';

const drawing = (over: Partial<BlockDrawing>): BlockDrawing => ({ boxes: [], passives: [], wires: [], grounds: [], flags: [], dots: [], frames: [], clusters: [], cells: [], width: 600, height: 400, ...over });
const pin = (name: string, net: string, side: PlacedPin['side'], x: number, y: number): PlacedPin => ({ name, net, side, x, y, connected: true }) as PlacedPin;
/** U2: its body 100..200 across, 100..200 down; pins out to 80 on the left and 220 on the right. */
const box = (pins: PlacedPin[]): Box => ({ x: 80, y: 100, w: 140, h: 100, body: { x: 100, y: 100, w: 100, h: 100 }, part: { id: 'U2', title: 'U2 MCU' } as DiagramPart, pins });
const wire = (net: string, path: string): Wire => ({ net, path });
const passive = (label: string, x: number, y: number): PlacedPassive => ({ x, y, w: 40, h: 40, passive: { id: label, label } as DiagramPassive, ends: [{ x, y }, { x, y: y + 40 }], terminals: ['1', '2'], label: { x, y, anchor: 'start' } });

describe('the layout checks', () => {
  it('reads the corners of a path', () => {
    expect(pathPoints('M10,20 H50 V80 L60,90')).toEqual([
      { x: 10, y: 20 },
      { x: 50, y: 20 },
      { x: 50, y: 80 },
      { x: 60, y: 90 },
    ]);
  });

  it('finds a wire that leaves a pin on the left and loops round to the right', () => {
    // The ESP32-C6 Boot strap: IO8 on the left, its pull-up right of the MCU.
    const d = drawing({ boxes: [box([pin('IO8', 'IO8_STRAP', 'left', 80, 150)])], wires: [wire('IO8_STRAP', 'M80,150 H60 V80 H260 V150 H300')] });
    expect(wireRoundPart.find(d).map((f) => f.text)).toEqual(["IO8_STRAP's wire leaves U2 MCU's IO8 on its left and runs round U2 MCU to its right"]);
  });

  it('follows the wire through a junction, but not into another net', () => {
    const d = drawing({
      boxes: [box([pin('IO8', 'IO8_STRAP', 'left', 80, 150)])],
      wires: [wire('IO8_STRAP', 'M80,150 H40'), wire('IO8_STRAP', 'M40,150 V60 H300'), wire('OTHER', 'M80,150 H400')],
    });
    expect(wireRoundPart.find(d)).toHaveLength(1);
    const apart = drawing({ boxes: [box([pin('IO8', 'IO8_STRAP', 'left', 80, 150)])], wires: [wire('IO8_STRAP', 'M80,150 H40'), wire('OTHER', 'M40,150 V60 H300')] });
    expect(wireRoundPart.find(apart)).toHaveLength(0);
  });

  it("doesn't count a wire staying on its pin's side, nor one from a top pin dropping beside the part", () => {
    const left = drawing({ boxes: [box([pin('EN', 'EN', 'left', 80, 150)])], wires: [wire('EN', 'M80,150 H20 V300')] });
    expect(wireRoundPart.find(left)).toHaveLength(0);
    const top = drawing({ boxes: [box([pin('VIN', 'VBUS', 'top', 150, 100)])], wires: [wire('VBUS', 'M150,100 V80 H40 V300')] });
    expect(wireRoundPart.find(top)).toHaveLength(0);
    const under = drawing({ boxes: [box([pin('VIN', 'VBUS', 'top', 150, 100)])], wires: [wire('VBUS', 'M150,100 V80 H40 V260 H150')] });
    expect(wireRoundPart.find(under)).toHaveLength(1);
  });

  it('finds a wire through a part, but not one ending at its pins', () => {
    const d = drawing({ boxes: [box([pin('PA0', 'A', 'left', 80, 150)])], wires: [wire('A', 'M80,150 H40'), wire('B', 'M40,120 H260')] });
    expect(wireThroughPart.find(d).map((f) => f.text)).toEqual(["B's wire runs through U2 MCU"]);
  });

  it('finds parts drawn on top of each other, not ones side by side', () => {
    const d = drawing({ boxes: [box([])], passives: [passive('R1', 180, 180), passive('R2', 300, 300), passive('R3', 340, 300)] });
    expect(overlappingParts.find(d).map((f) => f.text)).toEqual(['U2 MCU and R1 are drawn on top of each other']);
  });

  it('names each pair of crossing nets once', () => {
    const d = drawing({ wires: [wire('A', 'M0,50 H100'), wire('B', 'M50,0 V100'), wire('B', 'M70,0 V100'), wire('A', 'M60,0 V100'), wire('C', 'M0,100 H40')] });
    expect(crossingWires.find(d).map((f) => f.text)).toEqual(['A and B cross']);
  });

  it('runs every check and names the group each finding is in', () => {
    const frame = { x: 0, y: 0, w: 400, h: 300, header: 40, group: { name: 'MCU' } as DiagramGroup };
    const d = drawing({ frames: [frame], boxes: [box([pin('IO8', 'IO8_STRAP', 'left', 80, 150)])], wires: [wire('IO8_STRAP', 'M80,150 H60 V80 H260 V150 H300')] });
    expect(lint(d)).toEqual([expect.objectContaining({ check: 'wire round a part', group: 'MCU' })]);
    // A check of one's own joins the others.
    const always: LayoutCheck = { name: 'always', find: () => [{ text: 'here', at: { x: 500, y: 350 } }] };
    expect(lint(d, [always])).toEqual([{ check: 'always', text: 'here', at: { x: 500, y: 350 } }]);
  });
});
