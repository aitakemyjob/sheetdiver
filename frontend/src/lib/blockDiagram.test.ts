import { describe, expect, it } from 'vitest';
import { recorded } from '../test/backend';
import type { Diagram, ProjectDetail } from './types';
import { labelRect, layoutBlockDiagram, overlaps, separate, type BlockDrawing, type Box, type Pt, type Rect } from './blockDiagram';

/** A test circuit's diagram, as the backend built it (internal/testutil/circuits.go). */
const circuit = (name: string): Diagram => (recorded('project.detail', { project: name }) as ProjectDetail).connections.diagram;
const circuits = ['USB-C to 3V3', 'Battery board', 'MCU board', 'Tester board', 'Grouped MCU board'];

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

/** A wire's straight pieces, each horizontal or vertical. */
type Seg = { net: string; x1: number; y1: number; x2: number; y2: number };
function segments(d: BlockDrawing): Seg[] {
  return d.wires.flatMap((w) => {
    const pts = points(w.path);
    return pts.slice(1).map((p, i) => ({ net: w.net, x1: Math.min(p.x, pts[i].x), y1: Math.min(p.y, pts[i].y), x2: Math.max(p.x, pts[i].x), y2: Math.max(p.y, pts[i].y) }));
  });
}

/** Wires of different nets that cross (a vertical through a horizontal, inside both). */
function crossings(d: BlockDrawing): string[] {
  const segs = segments(d);
  const out: string[] = [];
  for (const v of segs.filter((s) => s.x1 === s.x2 && s.y1 !== s.y2))
    for (const h of segs.filter((s) => s.y1 === s.y2 && s.x1 !== s.x2))
      if (v.net !== h.net && v.x1 > h.x1 && v.x1 < h.x2 && h.y1 > v.y1 && h.y1 < v.y2) out.push(`${v.net} crosses ${h.net} at ${v.x1},${h.y1}`);
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
    expect(d.boxes.map((b) => b.part.id).sort()).toEqual(circuit(name).parts.map((p) => p.id).sort());
    expect(d.passives.map((p) => p.passive.id).sort()).toEqual(circuit(name).passives.map((p) => p.id).sort());
  });

  it('never overlaps', () => {
    const all = shapes(d);
    const clashes: string[] = [];
    all.forEach((a, i) => all.slice(i + 1).forEach((b) => overlaps(a, b) && clashes.push(`${a.what} / ${b.what}`)));
    expect(clashes).toEqual([]);
  });

  it("never crosses one net's wire with another's", () => {
    expect(crossings(d)).toEqual([]);
  });

  it("puts nothing on another net's wire", () => {
    const hits: string[] = [];
    for (const p of d.passives)
      for (const s of segments(d))
        if (!p.passive.nets.includes(s.net) && overlaps(p, { x: s.x1, y: s.y1, w: s.x2 - s.x1, h: s.y2 - s.y1 }, -1)) hits.push(`${p.passive.label} on ${s.net}`);
    expect(hits).toEqual([]);
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

  it('keeps a bridged passive between its pins', () => {
    for (const p of d.passives.filter((x) => x.passive.mount === 'bridge')) {
      const box = d.boxes.find((b) => b.part.id === p.passive.part)!;
      expect(p.x - (box.x + box.w), p.passive.label).toBeGreaterThan(0);
      expect(p.x - (box.x + box.w), p.passive.label).toBeLessThan(60);
    }
  });

  it('never puts a passive inline: its own wire only drops into it from above', () => {
    for (const p of d.passives.filter((x) => x.passive.mount !== 'bridge'))
      for (const s of segments(d).filter((x) => p.passive.nets.includes(x.net))) {
        const inside = overlaps(p, { x: s.x1, y: s.y1, w: s.x2 - s.x1, h: s.y2 - s.y1 }, -1);
        expect(inside, `${s.net} through ${p.passive.label}`).toBe(false);
      }
  });

  it('gives every passive to ground, and every ground pin, a ground symbol', () => {
    const shunts = d.passives.filter((p) => p.passive.mount !== 'bridge' && p.passive.nets.length === 1);
    const pins = d.boxes.flatMap((b) => b.pins.filter((p) => p.ground)).length;
    expect(d.grounds).toHaveLength(shunts.length + pins);
    for (const p of shunts) expect(d.grounds.some((g) => g.x === p.x + p.w / 2 && g.y > p.y + p.h)).toBe(true);
  });

  it('names the other end of a passive between two nets', () => {
    for (const p of d.passives.filter((x) => x.passive.mount !== 'bridge' && x.passive.nets.length === 2))
      expect(d.flags.some((f) => f.text === p.passive.nets[1] && f.x === p.x + p.w / 2)).toBe(true);
  });

  it('writes no text over a box or other text', () => {
    // Wire labels, and the pin names along each box's bottom.
    const texts = [
      ...d.wires.filter((w) => w.label).map((w) => ({ what: w.label!.text, ...labelRect(w.label!) })),
      ...d.boxes.flatMap((b) => b.pins.filter((p) => p.side === 'bottom').map((p) => ({ what: `${b.part.title} ${p.name}`, x: p.x - p.name.length * 3.5, y: p.y - 24, w: p.name.length * 7, h: 14 }))),
    ];
    const clashes: string[] = [];
    texts.forEach((a, i) => texts.slice(i + 1).forEach((b) => overlaps(a, b, -1) && clashes.push(`${a.what} / ${b.what}`)));
    for (const t of texts.slice(0, d.wires.filter((w) => w.label).length)) for (const b of d.boxes) if (overlaps(t, { x: b.x, y: b.y, w: b.w, h: b.h }, -1)) clashes.push(`${t.what} on ${b.part.title}`);
    expect(clashes).toEqual([]);
  });

  it('wires a pin tied to another to that pin\'s wire', () => {
    const ends = new Set(d.wires.flatMap((w) => points(w.path)).map((p) => `${p.x},${p.y}`));
    for (const b of d.boxes)
      for (const p of b.pins.filter((x) => x.tied && x.side !== 'inside')) {
        const to = b.pins.find((x) => x.name === p.tied && !x.tied)!;
        expect(Math.abs(p.y - to.y), `${b.part.title} ${p.name}`).toBe(30); // right below it
        expect(ends.has(`${p.x},${p.y}`)).toBe(true);
        expect(d.dots.some((dot) => dot.y === to.y && Math.abs(dot.x - p.x) === 10)).toBe(true);
      }
  });

  it('passes no wire through a box', () => {
    const through: string[] = [];
    for (const b of d.boxes)
      for (const s of segments(d))
        if (overlaps({ x: b.x + 1, y: b.y + 1, w: b.w - 2, h: b.h - 2 }, { x: s.x1, y: s.y1, w: s.x2 - s.x1, h: s.y2 - s.y1 })) through.push(`${s.net} through ${b.part.title}`);
    expect(through).toEqual([]);
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
    // The Rd resistors drop from short stubs on the CC pins.
    for (const [rd, cc] of [
      ['Rd1', 'CC1'],
      ['Rd2', 'CC2'],
    ]) {
      expect(passive(rd).x).toBeGreaterThan(pin('J1', cc).x);
      expect(passive(rd).y).toBeGreaterThan(pin('J1', cc).y);
    }
    // L1 between LX1 and LX2; C3,C4 drop off 3V3 beyond it.
    expect(passive('L1').x).toBeGreaterThan(box('U2').x + box('U2').w);
    expect(passive('C3,C4').x).toBeGreaterThan(passive('L1').x + passive('L1').w);
  });

  it('ends an output with an arrow', () => {
    expect(d.wires.find((w) => w.label?.text === '3V3')).toMatchObject({ arrow: true });
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
  it('draws a rail joining four parts as a bus from its driver, with junctions', () => {
    const d = layoutBlockDiagram(circuit('Battery board'));
    const rail = segments(d).filter((s) => s.net === '4.2V');
    // One vertical carries it, with a branch to each part it feeds.
    expect(rail.filter((s) => s.x1 === s.x2 && s.y2 - s.y1 > 100)).toHaveLength(1);
    expect(d.dots.length).toBeGreaterThan(0);
  });
});

describe('the MCU board', () => {
  const d = layoutBlockDiagram(circuit('MCU board'));
  it('draws the I2C pull-ups up to 3V3, not down to ground', () => {
    for (const r of ['R2', 'R3']) {
      const p = d.passives.find((x) => x.passive.label === r)!;
      expect(d.flags.find((f) => f.x === p.x + p.w / 2)).toMatchObject({ text: '3V3' });
      expect(d.grounds.some((g) => g.x === p.x + p.w / 2)).toBe(false);
    }
  });
  it('takes a wire to a part two columns away over or under the one between', () => {
    const u1 = d.boxes.find((b) => b.part.title.startsWith('U1'))!;
    for (const s of segments(d).filter((x) => x.net === '3V3' || x.net === 'D+' || x.net === 'D-'))
      expect(overlaps({ x: u1.x + 1, y: u1.y + 1, w: u1.w - 2, h: u1.h - 2 }, { x: s.x1, y: s.y1, w: s.x2 - s.x1, h: s.y2 - s.y1 })).toBe(false);
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

describe("the tester's board", () => {
  const dg = circuit('Tester board');
  const d = layoutBlockDiagram(dg);
  const box = (t: string) => d.boxes.find((b) => b.part.title.startsWith(t))!;
  const passive = (l: string) => d.passives.find((p) => p.passive.label === l)!;
  const pin = (t: string, n: string) => box(t).pins.find((p) => p.name === n)!;

  it('hangs R3 under the ILIM pin', () => {
    expect(passive('R3').y).toBeGreaterThan(pin('U1', 'ILIM').y);
    expect(Math.abs(passive('R3').x + passive('R3').w / 2 - pin('U1', 'ILIM').x)).toBeLessThan(80);
  });

  it('drops C3,C4 off 3V3 beyond L1, not on its wires', () => {
    expect(passive('C3,C4').x).toBeGreaterThan(passive('L1').x + passive('L1').w);
  });

  it("doesn't stretch a box with a long description", () => {
    expect(passive('C1,C2').passive.value).toBe('10µF');
    expect(passive('C1,C2').w).toBeLessThan(80);
  });

  it('keeps a passive where it was placed', () => {
    const moved = layoutBlockDiagram({ ...dg, passives: dg.passives.map((q) => (q.label === 'R3' ? { ...q, x: 300, y: 500 } : q)) });
    expect(moved.passives.find((p) => p.passive.label === 'R3')).toMatchObject({ x: 300, y: 500 });
  });
});


describe('a board in groups', () => {
  const dg = circuit('Grouped MCU board');
  const d = layoutBlockDiagram(dg);
  const frame = (name: string) => d.frames.find((f) => f.group.name === name)!;
  const box = (title: string) => d.boxes.find((b) => b.part.title.split(' ')[0] === title)!;
  const inside = (r: Rect, f: Rect) => r.x >= f.x && r.y >= f.y && r.x + r.w <= f.x + f.w && r.y + r.h <= f.y + f.h;

  it('frames each group, left to right in power-flow order, without overlapping', () => {
    expect(d.frames.map((f) => f.group.name)).toEqual(['Power', 'MCU', 'Sensor']);
    for (let i = 1; i < d.frames.length; i++) expect(d.frames[i].x).toBeGreaterThan(d.frames[i - 1].x + d.frames[i - 1].w);
    expect(frame('Power').group.description).toBe('USB 5 V to 3.3 V');
  });

  it('draws each part, and the passives hanging from it, inside its group, below the header', () => {
    for (const b of d.boxes) {
      const f = frame(b.part.group!);
      expect(inside({ x: b.x, y: b.y - 30, w: b.w, h: b.h + 30 }, f), b.part.title).toBe(true);
      expect(b.y - 30).toBeGreaterThan(f.y + f.header);
    }
    for (const p of d.passives) expect(inside(p, frame(p.passive.group!)), p.passive.label).toBe(true);
  });

  it('hangs each decoupling capacitor by the part it serves, not together on the rail', () => {
    const cap = (label: string) => d.passives.find((p) => p.passive.label === label)!;
    expect(inside(cap('C4'), frame('MCU'))).toBe(true);
    expect(inside(cap('C5'), frame('Sensor'))).toBe(true);
    // Each hangs from a wire leaving its own part's 3V3 pin.
    for (const [c, u] of [['C4', 'U2'], ['C5', 'U3']]) {
      const pin = box(u).pins.find((p) => p.net === '3V3' && !p.tied)!;
      const stub = d.wires.find((w) => w.net === '3V3' && w.path.startsWith(`M${pin.x},${pin.y} H`))!;
      const end = Number(/H(-?[\d.]+)/.exec(stub.path)![1]);
      const mid = cap(c).x + cap(c).w / 2;
      expect(mid, c).toBeGreaterThanOrEqual(Math.min(pin.x, end));
      expect(mid, c).toBeLessThanOrEqual(Math.max(pin.x, end));
    }
  });

  it('labels a net leaving its group, and runs no wire between groups', () => {
    for (const w of d.wires) {
      const xs = points(w.path).map((p) => p.x);
      const owners = d.frames.filter((f) => xs.some((x) => x >= f.x && x <= f.x + f.w));
      expect(owners.length, `${w.net}: ${w.path}`).toBeLessThanOrEqual(1);
    }
    const labels = d.wires.filter((w) => w.label?.text === '3V3');
    expect(labels).toHaveLength(3); // a stub in each group
  });

  it('keeps a part dragged in a group where it was put, relative to its frame', () => {
    const moved = layoutBlockDiagram({ ...dg, parts: dg.parts.map((p) => (p.title.startsWith('U3') ? { ...p, x: 300, y: 200 } : p)) });
    const u3 = moved.boxes.find((b) => b.part.title.startsWith('U3'))!;
    expect(u3.x).toBe(u3.origin!.x + 300);
    expect(u3.y).toBe(u3.origin!.y + 200);
    // The other groups don't move.
    for (const name of ['Power', 'MCU']) expect(moved.frames.find((f) => f.group.name === name)).toEqual(frame(name));
  });

  it('draws a board without groups as one drawing, unframed', () => {
    const plain = layoutBlockDiagram(circuit('MCU board'));
    expect(plain.frames).toEqual([]);
    expect(plain.boxes.every((b) => b.origin === undefined)).toBe(true);
  });
});

describe('a passive on a wire between two parts', () => {
  it('hangs by the part it supports, not at the start of the wire', () => {
    // C1,C2 sit on 5V_PROT between U1's OUT and U2's VIN, and serve U2.
    const dg = circuit('USB-C to 3V3');
    const d = layoutBlockDiagram(dg);
    const caps = d.passives.find((p) => p.passive.label === 'C1,C2')!;
    const u2 = d.boxes.find((b) => b.part.title.startsWith('U2'))!;
    const u1 = d.boxes.find((b) => b.part.title.startsWith('U1'))!;
    expect(caps.passive.part).toBe(u2.part.id);
    const vin = u2.pins.find((p) => p.net === '5V_PROT' && !p.tied)!;
    const out = u1.pins.find((p) => p.net === '5V_PROT' && !p.tied)!;
    const mid = caps.x + caps.w / 2;
    expect(Math.abs(mid - vin.x)).toBeLessThan(Math.abs(mid - out.x));
  });
});
