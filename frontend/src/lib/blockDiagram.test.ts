import { describe, expect, it } from 'vitest';
import { recorded } from '../test/backend';
import type { Diagram, ProjectDetail } from './types';
import { layoutBlockDiagram, overlaps, PITCH, separate, type BlockDrawing, type Box, type Pt } from './blockDiagram';

/** A test circuit's diagram, as the backend built it (internal/testutil/circuits.go). */
const circuit = (name: string): Diagram => (recorded('project.detail', { project: name }) as ProjectDetail).connections.diagram;
const circuits = ['USB-C to 3V3', 'Battery board', 'MCU board'];

/** The corner points of an SVG path of M, H, V and L commands. */
function points(path: string): Pt[] {
  const out: Pt[] = [];
  let x = 0;
  let y = 0;
  for (const [, cmd, a, b] of path.matchAll(/([MLHV])(-?[\d.]+)(?:,(-?[\d.]+))?/g)) {
    if (cmd === 'M' || cmd === 'L') [x, y] = [Number(a), Number(b)];
    if (cmd === 'H') x = Number(a);
    if (cmd === 'V') y = Number(a);
    out.push({ x, y });
  }
  return out;
}

/** Everything drawn as a rectangle: boxes with their titles, passives, net labels. */
function shapes(d: BlockDrawing) {
  return [
    ...d.boxes.map((b) => ({ what: b.part.title, x: b.x, y: b.y - 30, w: b.w, h: b.h + 30 })),
    ...d.passives.map((p) => ({ what: p.passive.label, x: p.x, y: p.y, w: p.w, h: p.h })),
  ];
}

describe.each(circuits)('the block diagram of %s', (name) => {
  const d = layoutBlockDiagram(circuit(name));

  it('draws every part and passive once', () => {
    expect(d.boxes.map((b) => b.part.id)).toEqual(circuit(name).parts.map((p) => p.id));
    expect(d.passives.map((p) => p.passive.id).sort()).toEqual(circuit(name).passives.map((p) => p.id).sort());
  });

  it('never overlaps', () => {
    const all = shapes(d);
    const clashes: string[] = [];
    all.forEach((a, i) => all.slice(i + 1).forEach((b) => overlaps(a, b) && clashes.push(`${a.what} / ${b.what}`)));
    expect(clashes).toEqual([]);
  });

  it('draws only right angles, inside the drawing', () => {
    for (const w of d.wires) {
      const pts = points(w.path);
      for (let i = 1; i < pts.length; i++) expect(pts[i].x === pts[i - 1].x || pts[i].y === pts[i - 1].y, `${w.net}: ${w.path}`).toBe(true);
      for (const p of pts) {
        expect(p.x).toBeGreaterThanOrEqual(0);
        expect(p.x).toBeLessThanOrEqual(d.width);
        expect(p.y).toBeLessThanOrEqual(d.height);
      }
    }
    for (const s of shapes(d)) expect(s.x + s.w).toBeLessThanOrEqual(d.width);
  });

  it('wires every pin on a side of its box', () => {
    const ends = new Set(d.wires.flatMap((w) => points(w.path)).map((p) => `${p.x},${p.y}`));
    for (const b of d.boxes)
      for (const p of b.pins.filter((x) => x.side !== 'inside')) {
        expect(ends.has(`${p.x},${p.y}`), `${b.part.title} ${p.name}`).toBe(true);
        // On its side of the box.
        if (p.side === 'left') expect(p.x).toBe(b.x);
        if (p.side === 'right') expect(p.x).toBe(b.x + b.w);
        if (p.side === 'bottom') expect(p.y).toBe(b.y + b.h);
      }
  });

  it('keeps passives near what they hang from', () => {
    for (const p of d.passives) {
      const box = d.boxes.find((b) => b.part.id === p.passive.part)!;
      if (p.passive.mount === 'beside' || p.passive.mount === 'bridge') {
        // Beside its box, not wandering off across the drawing.
        expect(p.x - (box.x + box.w), `${p.passive.label}`).toBeLessThan(100);
      }
    }
  });

  it('leaves room beside a box for a passive across its pins', () => {
    for (const p of d.passives.filter((x) => x.passive.mount === 'bridge')) {
      const box = d.boxes.find((b) => b.part.id === p.passive.part)!;
      for (const next of d.boxes.filter((b) => b.part.column > box.part.column)) expect(next.x - (p.x + p.w), `${p.passive.label} to ${next.part.title}`).toBeGreaterThan(60);
    }
  });

  it('puts boxes left to right by column', () => {
    for (const a of d.boxes) for (const b of d.boxes) if (a.part.column < b.part.column) expect(a.x).toBeLessThan(b.x);
  });
});

describe('the USB-C sketch', () => {
  const d = layoutBlockDiagram(circuit('USB-C to 3V3'));
  const box = (t: string) => d.boxes.find((b) => b.part.title.startsWith(t))!;
  const passive = (l: string) => d.passives.find((p) => p.passive.label === l)!;
  const pin = (t: string, n: string) => box(t).pins.find((p) => p.name === n)!;

  it('hangs passives where they serve', () => {
    // C1,C2 under the 5V_PROT wire, between U1 and U2.
    expect(passive('C1,C2').x).toBeGreaterThan(box('U1').x + box('U1').w);
    expect(passive('C1,C2').x + passive('C1,C2').w).toBeLessThan(box('U2').x);
    // R4 under its CFG pin, the Rd resistors beside J1's CC pins.
    expect(passive('R4').y).toBeGreaterThan(box('U2').y + box('U2').h);
    for (const [rd, cc] of [
      ['Rd1', 'CC1'],
      ['Rd2', 'CC2'],
    ]) {
      // Right beside the pin, whatever else hangs nearby (D1 on VBUS).
      expect(passive(rd).x - pin('J1', cc).x).toBeGreaterThan(0);
      expect(passive(rd).x - pin('J1', cc).x).toBeLessThan(40);
      expect(passive(rd).y + passive(rd).h / 2).toBe(pin('J1', cc).y);
    }
    // L1 across LX1 and LX2, which sit below the gap left for C3,C4.
    expect(passive('L1').x).toBeGreaterThan(box('U2').x + box('U2').w);
    expect(pin('U2', 'LX1').y - pin('U2', 'VOUT').y).toBeGreaterThan(3 * PITCH);
    expect(passive('C3,C4').y + passive('C3,C4').h).toBeLessThan(passive('L1').y);
  });

  it('ends an output with an arrow and notes the stubs to ground', () => {
    expect(d.wires.find((w) => w.label?.text === '3V3')).toMatchObject({ arrow: true });
    expect(d.grounded).toBe(true);
  });

  it('keeps where a part was dragged', () => {
    const dg = circuit('USB-C to 3V3');
    const moved = layoutBlockDiagram({ ...dg, parts: dg.parts.map((p) => (p.title.startsWith('U2') ? { ...p, x: 900, y: 300 } : p)) });
    expect(moved.boxes.find((b) => b.part.title.startsWith('U2'))).toMatchObject({ x: 900, y: 300 });
  });

  it('moves a part dropped onto another', () => {
    const dg = circuit('USB-C to 3V3');
    const u1 = box('U1');
    const moved = layoutBlockDiagram({ ...dg, parts: dg.parts.map((p) => (p.title.startsWith('U2') ? { ...p, x: u1.x + 20, y: u1.y + 10 } : p)) });
    const all = shapes(moved);
    all.forEach((a, i) => all.slice(i + 1).forEach((b) => expect(overlaps(a, b), `${a.what} / ${b.what}`).toBe(false)));
  });
});

describe('the battery board', () => {
  it('runs a rail joining four parts along a lane above them', () => {
    const d = layoutBlockDiagram(circuit('Battery board'));
    const lane = d.wires.find((w) => w.label?.text === '4.2V')!;
    const [start] = points(lane.path);
    for (const b of d.boxes) expect(start.y).toBeLessThan(b.y - 30);
  });
});

describe('separate', () => {
  it('moves overlapping boxes apart, and leaves the rest', () => {
    const box = (x: number, y: number) => ({ x, y, w: 200, h: 120, gap: 0, pins: [], part: { id: `${x},${y}`, title: '', kind: 'ic', column: 0, pins: [], ground: [] } }) as Box;
    const boxes = [box(0, 0), box(50, 40), box(600, 0)];
    separate(boxes);
    expect(overlaps(boxes[0], boxes[1])).toBe(false);
    expect(boxes[2]).toMatchObject({ x: 600, y: 0 });
  });
});
