import { beforeAll, describe, expect, it } from 'vitest';
import { recorded } from '../test/backend';
import type { Diagram, ProjectDetail } from './types';
import { labelRect, layoutBlockDiagram, overlaps, PIN_LEN, tiedTo, type BlockDrawing, type Box, type PlacedPin, type Pt, type Rect } from './blockDiagram';

/** A test circuit's diagram, as the backend built it (internal/testutil/circuits.go). */
const circuit = (name: string): Diagram => (recorded('project.detail', { project: name }) as ProjectDetail).connections.diagram;
const circuits = ['USB-C to 3V3', 'Battery board', 'MCU board', 'Tester board', 'Grouped MCU board', 'STM32 board', 'U545 USB-C board', 'Driver board', 'Boost board', 'Buck board'];

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

/** Where wires of different nets cross (a vertical through a horizontal, inside both), each place once. */
function crossings(d: BlockDrawing): string[] {
  const segs = segments(d);
  const out = new Set<string>();
  for (const v of segs.filter((s) => s.x1 === s.x2 && s.y1 !== s.y2))
    for (const h of segs.filter((s) => s.y1 === s.y2 && s.x1 !== s.x2))
      if (v.net !== h.net && v.x1 > h.x1 && v.x1 < h.x2 && h.y1 > v.y1 && h.y1 < v.y2) out.add(`${v.net} crosses ${h.net} at ${v.x1},${h.y1}`);
  return [...out];
}

/** Where a pin's name (inside the body) and number ("#", beside its line) are written, as the view draws them. */
function pinTexts(b: Box, p: PlacedPin): (Rect & { what: string })[] {
  const body = b.body;
  const name = p.name.length * 6.6;
  const num = (p.number ?? '').length * 5.4;
  const vertical = p.side === 'top' || p.side === 'bottom';
  const out: (Rect & { what: string })[] = [];
  if (vertical) out.push({ what: `${b.part.title} ${p.name}`, x: p.x - 6, y: p.side === 'top' ? body.y + 6 : body.y + body.h - 6 - name, w: 12, h: name });
  else out.push({ what: `${b.part.title} ${p.name}`, x: p.side === 'left' ? body.x + 6 : body.x + body.w - 6 - name, y: p.y - 6, w: name, h: 12 });
  if (!p.number) return out;
  if (vertical) out.push({ what: `${b.part.title} ${p.name}#`, x: p.x + 3, y: p.side === 'top' ? body.y - 13 : body.y + body.h + 2, w: num, h: 10 });
  else out.push({ what: `${b.part.title} ${p.name}#`, x: (p.x + (p.side === 'left' ? body.x : body.x + body.w)) / 2 - num / 2, y: p.y - 12, w: num, h: 10 });
  return out;
}

/** Whether a rectangle lies wholly within a frame. */
const inside = (r: Rect, f: Rect) => r.x >= f.x && r.y >= f.y && r.x + r.w <= f.x + f.w && r.y + r.h <= f.y + f.h;

/** Everything drawn as a rectangle: boxes with their titles, passives, net labels. */
function shapes(d: BlockDrawing) {
  return [
    ...d.boxes.map((b) => ({ what: b.part.title, x: b.x, y: b.y - 30, w: b.w, h: b.h + 30 })),
    ...d.passives.map((p) => ({ what: p.passive.label, x: p.x, y: p.y, w: p.w, h: p.h })),
  ];
}

describe.each(circuits)('the block diagram of %s', (name) => {
  let d: BlockDrawing;
  beforeAll(async () => {
    d = await layoutBlockDiagram(circuit(name));
  });

  it('draws every part and passive once', async () => {
    expect(d.boxes.map((b) => b.part.id).sort()).toEqual(circuit(name).parts.map((p) => p.id).sort());
    expect(d.passives.map((p) => p.passive.id).sort()).toEqual(circuit(name).passives.map((p) => p.id).sort());
  });

  it('never overlaps', () => {
    const all = shapes(d);
    const clashes: string[] = [];
    all.forEach((a, i) => all.slice(i + 1).forEach((b) => overlaps(a, b) && clashes.push(`${a.what} / ${b.what}`)));
    expect(clashes).toEqual([]);
  });

  it("seldom crosses one net's wire with another's", () => {
    // ELK keeps crossings to a minimum; where a net's branches interleave
    // with another's (two pull-ups on an I2C bus) it can't always avoid
    // one. Pins sit where the symbol puts them, so a board whose MCU has
    // many pins in play gets one more.
    expect(crossings(d).length).toBeLessThanOrEqual(name === 'STM32 board' ? 3 : 2);
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

  it('wires every connected pin at the end of its pin line, and no unconnected one', () => {
    const ends = new Set(d.wires.flatMap((w) => points(w.path)).map((p) => `${p.x},${p.y}`));
    for (const b of d.boxes)
      for (const p of b.pins) {
        expect(ends.has(`${p.x},${p.y}`), `${b.part.title} ${p.name}`).toBe(!p.dim);
        // At the node's border, a pin line out from the body.
        if (p.side === 'left') expect(p.x).toBe(b.x);
        if (p.side === 'right') expect(p.x).toBe(b.x + b.w);
        if (p.side === 'top') expect(p.y).toBe(b.y);
        if (p.side === 'bottom') expect(p.y).toBe(b.y + b.h);
        // A transistor is its symbol: the pin ends on its border.
        expect(b.body.x - b.x, b.part.title).toBe(b.part.shape === 'ic' ? PIN_LEN : 0);
      }
  });

  it('shows every pin of a part with a pin table, once, in its number order on each side', () => {
    const detail = recorded('project.detail', { project: name }) as ProjectDetail;
    for (const b of d.boxes.filter((x) => x.part.full)) {
      const table = detail.connections.pinouts.find((po) => po.node === b.part.id)!;
      expect(b.pins.length, b.part.title).toBe(table.pins.length);
      for (const side of ['left', 'right', 'top', 'bottom'] as const) {
        const nums = b.pins.filter((p) => p.side === side && /^\d+$/.test(p.number ?? '')).map((p) => Number(p.number));
        expect(nums, `${b.part.title} ${side}`).toEqual([...nums].sort((a, z) => a - z));
      }
    }
  });

  it('keeps a bridged passive between its pins', () => {
    for (const p of d.passives.filter((x) => x.passive.mount === 'bridge')) {
      const box = d.boxes.find((b) => b.part.id === p.passive.part)!;
      const pin = box.pins.find((x) => x.net === p.passive.nets[0] && !x.tied)!;
      const gap = pin.side === 'left' ? box.x - (p.x + p.w) : p.x - (box.x + box.w);
      expect(gap, p.passive.label).toBeGreaterThan(0);
      expect(gap, p.passive.label).toBeLessThan(160);
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
    for (const p of shunts) expect(d.grounds.some((g) => g.x === p.ends[1].x && g.y > p.ends[1].y)).toBe(true);
  });

  it('names the other end of a passive between two nets, unless both are wired', () => {
    for (const p of d.passives.filter((x) => x.passive.mount !== 'bridge' && x.passive.nets.length === 2)) {
      const flagged = d.flags.some((f) => p.passive.nets.includes(f.text) && f.x === p.ends[1].x && f.y > p.ends[1].y);
      const wired = p.passive.nets.every((n) => d.wires.some((w) => w.net === n && !w.thin && points(w.path).some((pt) => overlaps(p, { x: pt.x, y: pt.y, w: 0, h: 0 }, 1))));
      expect(flagged || wired, p.passive.label).toBe(true);
    }
  });

  it('writes no text over a box or other text', () => {
    // Wire labels, and the pin names and numbers of each symbol.
    const texts = [
      ...d.wires.filter((w) => w.label).map((w) => ({ what: w.label!.text, ...labelRect(w.label!) })),
      ...d.boxes.flatMap((b) => b.pins.flatMap((p) => pinTexts(b, p))),
    ];
    const clashes: string[] = [];
    texts.forEach((a, i) => texts.slice(i + 1).forEach((b) => overlaps(a, b, -1) && clashes.push(`${a.what} / ${b.what}`)));
    for (const t of texts.slice(0, d.wires.filter((w) => w.label).length)) for (const b of d.boxes) if (overlaps(t, { x: b.x, y: b.y, w: b.w, h: b.h }, -1)) clashes.push(`${t.what} on ${b.part.title}`);
    expect(clashes).toEqual([]);
  });

  it("wires a pin tied to another to that pin's wire", () => {
    const ends = new Set(d.wires.flatMap((w) => points(w.path)).map((p) => `${p.x},${p.y}`));
    for (const b of d.boxes)
      for (const p of b.pins.filter((x) => x.tied)) {
        const to = tiedTo(b, p)!;
        const ties = b.pins.filter((x) => x.tied === p.tied && x.side === p.side).length;
        if (p.side === 'left' || p.side === 'right') {
          // Right below it, after any other pins tied to it.
          expect(p.y - to.y, `${b.part.title} ${p.name}`).toBeGreaterThan(0);
          expect((p.y - to.y) % 30, `${b.part.title} ${p.name}`).toBe(0);
          expect(p.y - to.y, `${b.part.title} ${p.name}`).toBeLessThanOrEqual(30 * ties);
          expect(d.dots.some((dot) => dot.y === to.y && Math.abs(dot.x - p.x) === 10)).toBe(true);
        } else {
          // Beside it along the top, its jog meeting the main pin's wire.
          expect(Math.abs(p.x - to.x) % 30, `${b.part.title} ${p.name}`).toBe(0);
          expect(d.dots.some((dot) => dot.x === to.x && Math.abs(dot.y - p.y) === 10)).toBe(true);
        }
        expect(ends.has(`${p.x},${p.y}`)).toBe(true);
      }
  });

  it("attaches a wire, or its far end's ground or name, at each end of every passive's symbol", () => {
    const ends = new Set(d.wires.flatMap((w) => points(w.path)).map((p) => `${p.x},${p.y}`));
    for (const p of d.passives) {
      for (const e of p.ends) expect(ends.has(`${e.x},${e.y}`), `${p.passive.label} at ${e.x},${e.y}`).toBe(true);
      // The symbol runs down the node, or along it.
      if (p.horizontal) expect(p.ends[0].y).toBe(p.ends[1].y);
      else expect(p.ends[0].x).toBe(p.ends[1].x);
      // Its terminals are the backend's, the first at the net its first end is on.
      expect([...p.terminals].sort()).toEqual([...p.passive.ends].sort());
    }
  });

  it("moves a net's hanging parts aside all together, or wires them all at the pin", () => {
    // Flagged with its net's name above it, or on a bus in a subgroup.
    const aside = (p: (typeof d.passives)[number]) => d.flags.some((f) => f.up && f.x === p.ends[0].x && f.y < p.ends[0].y) || d.clusters.some((c) => inside(p, c));
    // By the net at its top end (a pull-up hangs from its signal), within its group.
    const hangsFrom = (p: (typeof d.passives)[number]) => d.wires.find((w) => points(w.path).some((pt) => pt.x === p.ends[0].x && pt.y === p.ends[0].y))?.net ?? '';
    const byNet = new Map<string, boolean[]>();
    for (const p of d.passives.filter((x) => x.passive.mount !== 'bridge' && !x.horizontal)) {
      const key = `${p.passive.group ?? ''}|${hangsFrom(p)}`;
      byNet.set(key, [...(byNet.get(key) ?? []), aside(p)]);
    }
    for (const [net, flags] of byNet) expect(new Set(flags).size, `${net}: ${flags.join(',')}`).toBe(1);
  });

  it("writes no pin number over a wire", () => {
    const segs = segments(d);
    const hits: string[] = [];
    for (const b of d.boxes)
      for (const p of b.pins.filter((x) => x.number))
        for (const r of pinTexts(b, p).filter((x) => x.what.endsWith('#')))
          for (const s of segs) if (overlaps(r, { x: s.x1, y: s.y1, w: s.x2 - s.x1, h: s.y2 - s.y1 }, -1)) hits.push(`${b.part.title} ${p.name} under ${s.net}`);
    expect(hits).toEqual([]);
  });

  it('hides unconnected pins but supplies and grounds when asked for connected pins only', async () => {
    const compact = await layoutBlockDiagram(circuit(name), { connectedOnly: true });
    for (const b of compact.boxes)
      for (const p of b.pins.filter((x) => x.dim)) expect(['power', 'ground', 'pad'], `${b.part.title} ${p.name}`).toContain(p.role);
    // Nothing else changes about what's drawn.
    expect(compact.passives.length).toBe(d.passives.length);
  });

  it('passes no wire through a box', () => {
    const through: string[] = [];
    for (const b of d.boxes)
      for (const s of segments(d))
        if (overlaps({ x: b.x + 1, y: b.y + 1, w: b.w - 2, h: b.h - 2 }, { x: s.x1, y: s.y1, w: s.x2 - s.x1, h: s.y2 - s.y1 })) through.push(`${s.net} through ${b.part.title}`);
    expect(through).toEqual([]);
  });

  it('draws no wire of zero length, and no labelled wire shorter than its label', () => {
    const short: string[] = [];
    for (const w of d.wires) {
      const pts = points(w.path);
      if (pts.slice(1).some((p, i) => p.x === pts[i].x && p.y === pts[i].y)) short.push(`${w.net}: ${w.path}`);
      // A label in the middle of a wire sits over one of its pieces, or
      // at the end of a stub up or down from a pin.
      if (w.label?.anchor === 'middle' && !w.thin) {
        const r = labelRect(w.label);
        const under = pts.slice(1).some((p, i) => p.y === pts[i].y && Math.abs(p.y - w.label!.y) < 30 && Math.min(p.x, pts[i].x) <= r.x + 2 && Math.max(p.x, pts[i].x) >= r.x + r.w - 2);
        const atEnd = pts.slice(1).some((p, i) => p.x === pts[i].x && p.x === w.label!.x && Math.abs(p.y - w.label!.y) < 20);
        if (!under && !atEnd) short.push(`${w.net} label on ${w.path}`);
      }
    }
    expect(short).toEqual([]);
  });

  it("never lets two nets' wires touch, so none looks shorted to another", () => {
    const segs = segments(d);
    const touch: string[] = [];
    const on = (s: Seg, x: number, y: number) => x >= s.x1 && x <= s.x2 && y >= s.y1 && y <= s.y2;
    segs.forEach((a, i) =>
      segs.slice(i + 1).forEach((b) => {
        if (a.net === b.net) return;
        const ends = [
          [b, a.x1, a.y1],
          [b, a.x2, a.y2],
          [a, b.x1, b.y1],
          [a, b.x2, b.y2],
        ] as const;
        if (ends.some(([s, x, y]) => on(s, x, y))) touch.push(`${a.net} / ${b.net} at ${a.x1},${a.y1}–${a.x2},${a.y2} and ${b.x1},${b.y1}–${b.x2},${b.y2}`);
      }),
    );
    expect(touch).toEqual([]);
  });

  it("runs no wire along a box's side, where its pins are", () => {
    const along: string[] = [];
    for (const b of d.boxes)
      for (const s of segments(d))
        if (s.x1 === s.x2 && (s.x1 === b.x || s.x1 === b.x + b.w) && s.y2 > b.y && s.y1 < b.y + b.h && s.y1 !== s.y2) along.push(`${s.net} along ${b.part.title}`);
    expect(along).toEqual([]);
  });
});

describe('the USB-C sketch', () => {
  let d: BlockDrawing;
  beforeAll(async () => {
    d = await layoutBlockDiagram(circuit('USB-C to 3V3'));
  });
  const box = (t: string) => d.boxes.find((b) => b.part.title.startsWith(t))!;
  const passive = (l: string) => d.passives.find((p) => p.passive.label === l)!;
  const pin = (t: string, n: string) => box(t).pins.find((p) => p.name === n)!;

  it('hangs passives where they serve', () => {
    // C1,C2 on 5V_PROT after U1, by U2 (the part they serve).
    expect(passive('C1,C2').x).toBeGreaterThan(box('U1').x + box('U1').w);
    expect(passive('C1,C2').x).toBeLessThan(box('U2').x + box('U2').w);
    // R4 right under its CFG pin.
    expect(passive('R4').y).toBeGreaterThan(box('U2').y + box('U2').h);
    expect(Math.abs(passive('R4').ends[0].x - pin('U2', 'CFG1').x)).toBeLessThan(1);
    // The Rd resistors beyond J1's CC pins.
    for (const [rd, cc] of [
      ['Rd1', 'CC1'],
      ['Rd2', 'CC2'],
    ])
      expect(passive(rd).x).toBeGreaterThan(pin('J1', cc).x);
    // L1 beyond U2's LX pins; C3,C4 off 3V3 beyond U2.
    expect(passive('L1').x).toBeGreaterThan(box('U2').x + box('U2').w);
    expect(passive('C3,C4').x).toBeGreaterThan(box('U2').x + box('U2').w);
  });

  it('ends an output with an arrow', () => {
    expect(d.wires.find((w) => w.label?.text === '3V3')).toMatchObject({ arrow: true });
  });

  it('puts a part dragged above the others beside it first, and keeps it in its place in the flow', async () => {
    // U1, D1 and the Rd resistors all follow J1; U1 dragged to the top
    // goes above them, and stays after J1 and before U2 however far it's dragged.
    const dg = circuit('USB-C to 3V3');
    const moved = await layoutBlockDiagram({ ...dg, parts: dg.parts.map((p) => (p.title.startsWith('U1') ? { ...p, x: 5000, y: 0 } : p)) });
    const u1 = moved.boxes.find((b) => b.part.title.startsWith('U1'))!;
    for (const q of moved.passives.filter((p) => ['D1', 'Rd1', 'Rd2'].includes(p.passive.label))) expect(u1.y, q.passive.label).toBeLessThan(q.y);
    expect(u1.x).toBeGreaterThan(moved.boxes.find((b) => b.part.title.startsWith('J1'))!.x);
    expect(u1.x).toBeLessThan(moved.boxes.find((b) => b.part.title.startsWith('U2'))!.x);
  });

  it('moves a part dropped onto another', async () => {
    const dg = circuit('USB-C to 3V3');
    const u1 = box('U1');
    const moved = await layoutBlockDiagram({ ...dg, parts: dg.parts.map((p) => (p.title.startsWith('U2') ? { ...p, x: u1.x + 20, y: u1.y + 10 } : p)) });
    const all = shapes(moved);
    all.forEach((a, i) => all.slice(i + 1).forEach((b) => expect(overlaps(a, b), `${a.what} / ${b.what}`).toBe(false)));
  });
});

describe('the battery board', () => {
  it('names a rail joining four parts at each pin, as a schematic does, rather than wiring round them all', async () => {
    const d = await layoutBlockDiagram(circuit('Battery board'));
    const pins = d.boxes.flatMap((b) => b.pins.filter((p) => p.net === '4.2V' && !p.tied && !p.dim).map((p) => ({ part: b.part.title, ...p })));
    expect(pins.length).toBe(4);
    for (const p of pins) {
      const stub = d.wires.find((w) => w.net === '4.2V' && w.label?.text === '4.2V' && points(w.path)[0].x === p.x && points(w.path)[0].y === p.y);
      expect(stub, `${p.part} ${p.name}`).toBeDefined();
      expect(Math.abs(points(stub!.path).at(-1)!.x - p.x) + Math.abs(points(stub!.path).at(-1)!.y - p.y)).toBeLessThan(120);
    }
    // No long wire carries it.
    expect(segments(d).filter((s) => s.net === '4.2V' && s.x2 - s.x1 + s.y2 - s.y1 > 120)).toEqual([]);
  });
});

describe('the MCU board', () => {
  let d: BlockDrawing;
  beforeAll(async () => {
    d = await layoutBlockDiagram(circuit('MCU board'));
  });
  it('draws the I2C pull-ups up to 3V3, not down to ground', async () => {
    for (const r of ['R2', 'R3']) {
      const p = d.passives.find((x) => x.passive.label === r)!;
      expect(d.flags.find((f) => f.x === p.ends[1].x)).toMatchObject({ text: '3V3' });
      expect(d.grounds.some((g) => g.x === p.ends[1].x && g.y === p.ends[1].y + 6)).toBe(false);
    }
  });
  it('takes a wire to a part two columns away over or under the one between', async () => {
    const u1 = d.boxes.find((b) => b.part.title.startsWith('U1'))!;
    for (const s of segments(d).filter((x) => x.net === '3V3' || x.net === 'D+' || x.net === 'D-'))
      expect(overlaps({ x: u1.x + 1, y: u1.y + 1, w: u1.w - 2, h: u1.h - 2 }, { x: s.x1, y: s.y1, w: s.x2 - s.x1, h: s.y2 - s.y1 })).toBe(false);
  });
});

describe("the tester's board", () => {
  const dg = circuit('Tester board');
  let d: BlockDrawing;
  beforeAll(async () => {
    d = await layoutBlockDiagram(dg);
  });
  const box = (t: string) => d.boxes.find((b) => b.part.title.startsWith(t))!;
  const passive = (l: string) => d.passives.find((p) => p.passive.label === l)!;
  const pin = (t: string, n: string) => box(t).pins.find((p) => p.name === n)!;

  it('hangs R3 under the ILIM pin', () => {
    expect(passive('R3').y).toBeGreaterThan(pin('U1', 'ILIM').y);
    expect(Math.abs(passive('R3').ends[0].x - pin('U1', 'ILIM').x)).toBeLessThan(80);
  });

  it('drops C3,C4 off 3V3 beyond U2, not on its wires', () => {
    expect(passive('C3,C4').x).toBeGreaterThan(box('U2').x + box('U2').w);
  });

  it("doesn't stretch a box with a long description", () => {
    expect(passive('C1,C2').passive.value).toBe('10µF');
    expect(passive('C1,C2').w).toBeLessThan(80);
  });

  it('puts a passive dragged below the others beside it last', async () => {
    // D1 and the Rd resistors all hang off J1; D1 dragged to the bottom goes below them.
    const moved = await layoutBlockDiagram({ ...dg, passives: dg.passives.map((q) => (q.label === 'D1' ? { ...q, x: passive('D1').x, y: 2000 } : q)) });
    const d1 = moved.passives.find((p) => p.passive.label === 'D1')!;
    for (const rd of ['R1', 'R2']) expect(d1.y, rd).toBeGreaterThan(moved.passives.find((p) => p.passive.label === rd)!.y);
  });
});

describe('subgroups', () => {
  let d: BlockDrawing;
  beforeAll(async () => {
    d = await layoutBlockDiagram(circuit('Driver board'));
  });
  const cluster = (name: string) => d.clusters.find((c) => c.name === name)!;
  const passive = (label: string) => d.passives.find((p) => p.passive.label === label)!;
  const box = (title: string) => d.boxes.find((b) => b.part.title === title)!;

  it('draws the parts named into one inside its frame, wired up, inside their group', () => {
    const c = cluster('Status LED');
    expect(c).toBeDefined();
    for (const p of [passive('R3'), passive('D2'), box('Q2')]) expect(inside(p, c), p.x.toString()).toBe(true);
    const frame = d.frames.find((f) => f.group.name === 'MCU')!;
    expect(inside(c, frame)).toBe(true);
    // The LED is wired to the transistor's collector, not standing aside.
    const led = passive('D2');
    expect(d.wires.some((w) => w.net === 'LED2_K' && points(w.path).some((pt) => pt.x === led.ends[0].x && pt.y === led.ends[0].y))).toBe(true);
    expect(d.flags.some((f) => f.text === 'LED2_K')).toBe(false);
    // Its signal reaches it from the MCU's pin by a wire, to its border.
    const ctrl = d.wires.filter((w) => w.net === 'CTRL' && !w.thin).flatMap((w) => points(w.path));
    const pa3 = box('U2 MCU').pins.find((p) => p.name === 'PA3')!;
    expect(ctrl.some((pt) => pt.x === pa3.x && pt.y === pa3.y)).toBe(true);
    expect(ctrl.some((pt) => pt.x === c.x || pt.x === c.x + c.w)).toBe(true);
  });

  it("wires a net's hanging parts to the pin they hang off, on its side of the part", () => {
    // The button and its pull-up both hang off PA2's wire, named BTN; the
    // pull-up's rail is named at its far end; the switch goes to ground.
    const pa2 = box('U2 MCU').pins.find((p) => p.name === 'PA2')!;
    const btn = d.wires.filter((w) => w.net === 'BTN' && !w.thin).flatMap((w) => points(w.path));
    expect(btn.some((pt) => pt.x === pa2.x && pt.y === pa2.y)).toBe(true);
    for (const p of [passive('SW1'), passive('R2')]) {
      expect(btn.some((pt) => pt.x === p.ends[0].x && pt.y === p.ends[0].y), p.passive.label).toBe(true);
      expect(p.x + p.w, `${p.passive.label} left of the MCU`).toBeLessThan(box('U2 MCU').x);
    }
    expect(d.wires.some((w) => w.net === 'BTN' && w.label?.text === 'BTN')).toBe(true);
    expect(d.flags.some((f) => f.text === '3V3' && f.x === passive('R2').ends[1].x)).toBe(true);
    expect(d.grounds.some((g) => g.x === passive('SW1').ends[1].x)).toBe(true);
  });

  it('draws a passive reached only through another on a bus after it, led in from the pin', async () => {
    const b = await layoutBlockDiagram(circuit('Boost board'));
    const passive = (label: string) => b.passives.find((p) => p.passive.label === label)!;
    const box = (title: string) => b.boxes.find((t) => t.part.title.startsWith(title))!;
    for (const [lead, hanger, bus, pin, part] of [
      ['L1', 'C2', 'VOUT', 'SW', 'U1'],
      ['R5', 'D1', 'LEDK', 'PA5', 'U2'],
    ]) {
      const l = passive(lead);
      const h = passive(hanger);
      const c = b.clusters.find((x) => inside(h, x))!;
      expect(c, `${hanger} in a subgroup`).toBeDefined();
      expect(inside(l, c), `${lead} with it`).toBe(true);
      // The lead runs along the bus line into it; the hanger hangs off the bus.
      expect(l.horizontal).toBe(true);
      const wire = b.wires.find((w) => w.net === bus && w.label?.text === bus)!;
      const pts = points(wire.path);
      expect(pts.some((pt) => pt.x === l.ends[1].x && pt.y === l.ends[1].y), `${bus} from ${lead}`).toBe(true);
      expect(pts.some((pt) => pt.x === h.ends[0].x && pt.y === h.ends[0].y), `${bus} to ${hanger}`).toBe(true);
      // The pin's wire reaches the subgroup's border, and on in to the lead.
      const p = box(part).pins.find((x) => x.name === pin)!;
      const net = b.wires.filter((w) => w.net === p.net).flatMap((w) => points(w.path));
      expect(net.some((pt) => pt.x === p.x && pt.y === p.y), `${pin} wired`).toBe(true);
      expect(net.some((pt) => pt.x === l.ends[0].x && pt.y === l.ends[0].y), `${lead} wired`).toBe(true);
    }
  });

  it('takes a connector, and a chained passive with its lead, into a named subgroup', async () => {
    const b = await layoutBlockDiagram(circuit('Buck board'));
    const passive = (label: string) => b.passives.find((p) => p.passive.label === label)!;
    const box = (title: string) => b.boxes.find((t) => t.part.title.startsWith(title))!;
    const frame = (name: string) => b.frames.find((f) => f.group.name === name)!;
    // J2 alone in "Out", inside the Output group's frame, its 3V3 named at its pin.
    const out = b.clusters.find((c) => c.name === 'Out')!;
    expect(inside(box('J2'), out)).toBe(true);
    expect(inside(out, frame('Output'))).toBe(true);
    // D2 named into "LED" brings R4, which leads in from POK.
    const led = b.clusters.find((c) => c.name === 'LED')!;
    expect(inside(passive('D2'), led)).toBe(true);
    expect(inside(passive('R4'), led)).toBe(true);
    expect(passive('R4').horizontal).toBe(true);
    // C3 is in Power with U1, which its net reaches only through L1: on a bus after it.
    const c3 = b.clusters.find((c) => inside(passive('C3'), c))!;
    expect(inside(c3, frame('Power'))).toBe(true);
    expect(inside(passive('L1'), c3)).toBe(true);
    // The TVS is wired at the connector it protects.
    const d1 = passive('D1');
    const vbus = box('J1').pins.find((p) => p.net === 'VBUS')!;
    const pts = b.wires.filter((w) => w.net === 'VBUS' && !w.thin).flatMap((w) => points(w.path));
    expect(pts.some((pt) => pt.x === vbus.x && pt.y === vbus.y) && pts.some((pt) => pt.x === d1.ends[0].x && pt.y === d1.ends[0].y)).toBe(true);
  });

  it('leaves a part alone on its net where it was', () => {
    // D1 hangs under PA1; C2 stands by VDD with 3V3 flagged above it.
    expect(d.clusters.some((c) => inside(passive('D1'), c) || inside(passive('C2'), c))).toBe(false);
  });
});

describe('a board in groups', () => {
  const dg = circuit('Grouped MCU board');
  let d: BlockDrawing;
  beforeAll(async () => {
    d = await layoutBlockDiagram(dg);
  });
  const frame = (name: string) => d.frames.find((f) => f.group.name === name)!;
  const box = (title: string) => d.boxes.find((b) => b.part.title.split(' ')[0] === title)!;

  it('frames each group, left to right in power-flow order, without overlapping', async () => {
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
    // Nothing in these groups drives 3V3 (the Power group makes it), so
    // it isn't wired between parts: each part's 3V3 pin wires its own
    // capacitor, named 3V3 on the way, the capacitor beside its part on
    // its pin's side; no 3V3 wire reaches another part.
    for (const [c, u] of [['C4', 'U2'], ['C5', 'U3']]) {
      const pin = box(u).pins.find((p) => p.net === '3V3' && !p.tied)!;
      const top = cap(c).ends[0];
      const wire = d.wires.find((w) => w.net === '3V3' && !w.thin && points(w.path).some((pt) => pt.x === pin.x && pt.y === pin.y))!;
      expect(wire, `${u} wired`).toBeDefined();
      expect(points(wire.path).some((pt) => pt.x === top.x && pt.y === top.y), `${u} to ${c}`).toBe(true);
      expect(wire.label?.text).toBe('3V3');
      if (pin.side === 'left') expect(cap(c).x + cap(c).w, `${c} left of ${u}`).toBeLessThan(box(u).x);
      else expect(cap(c).x, `${c} right of ${u}`).toBeGreaterThan(box(u).x + box(u).w);
    }
    const pins3v3 = d.boxes.flatMap((b) => b.pins.filter((p) => p.net === '3V3' && !p.tied).map((p) => ({ ...p, box: b })));
    for (const w of d.wires.filter((x) => x.net === '3V3' && !x.thin)) {
      const at = pins3v3.filter((p) => points(w.path).some((pt) => pt.x === p.x && pt.y === p.y));
      expect(at.length, 'one part per 3V3 wire').toBeLessThanOrEqual(1);
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

  it('lays a group out again around a dragged part, leaving the other groups alone', async () => {
    // C5 dragged to the bottom of the Sensor group goes below the rest of it.
    const c5 = d.passives.find((p) => p.passive.label === 'C5')!;
    const moved = await layoutBlockDiagram({ ...dg, passives: dg.passives.map((q) => (q.label === 'C5' ? { ...q, x: c5.x - c5.origin!.x, y: 1000 } : q)) });
    const sensor = moved.passives.filter((p) => p.passive.group === 'Sensor');
    const low = sensor.find((p) => p.passive.label === 'C5')!;
    for (const p of sensor) expect(low.y, p.passive.label).toBeGreaterThanOrEqual(p.y);
    for (const name of ['Power', 'MCU']) expect(moved.frames.find((f) => f.group.name === name)).toEqual(frame(name));
  });

  it('draws a board without groups as one drawing, unframed', async () => {
    const plain = await layoutBlockDiagram(circuit('MCU board'));
    expect(plain.frames).toEqual([]);
    expect(plain.boxes.every((b) => b.origin === undefined)).toBe(true);
  });
});

describe('a passive on a wire between two parts', () => {
  it('hangs by the part it supports, not at the start of the wire', async () => {
    // C1,C2 sit on 5V_PROT between U1's OUT and U2's VIN, and serve U2.
    const dg = circuit('USB-C to 3V3');
    const d = await layoutBlockDiagram(dg);
    const caps = d.passives.find((p) => p.passive.label === 'C1,C2')!;
    const u2 = d.boxes.find((b) => b.part.title.startsWith('U2'))!;
    const u1 = d.boxes.find((b) => b.part.title.startsWith('U1'))!;
    expect(caps.passive.part).toBe(u2.part.id);
    // After U1, in U2's layer or the one before it.
    expect(caps.x).toBeGreaterThan(u1.x + u1.w);
    expect(caps.x).toBeLessThan(u2.x + u2.w);
  });
});
