import { describe, expect, it } from 'vitest';
import { cells, columns, hints, keepSides, type Placed } from './cells';

const n = (id: string, x: number, y: number, w = 40, h = 40): Placed => ({ id, x, y, w, h });
// Three columns: A; B over C; D.
const layout = [n('A', 0, 50), n('B', 130, 0), n('C', 120, 100, 60), n('D', 260, 50)];

describe('the grid of parts', () => {
  it('puts nodes whose spans overlap in one column, top to bottom', () => {
    expect(columns(layout).map((c) => c.map((x) => x.id))).toEqual([['A'], ['B', 'C'], ['D']]);
    expect(Object.fromEntries(cells(layout))).toEqual({ A: { column: 0, row: 0 }, B: { column: 1, row: 0 }, C: { column: 1, row: 1 }, D: { column: 2, row: 0 } });
  });

  it('keeps nodes where they are when nothing is asked for', () => {
    const h = hints(layout, new Map());
    expect(Object.fromEntries(h)).toEqual({ A: { x: 0, y: 50 }, B: { x: 130, y: 0 }, C: { x: 120, y: 100 }, D: { x: 260, y: 50 } });
  });

  it('centres a node asking for a column on it, so the layout puts it there', () => {
    const h = hints(layout, new Map([['D', { column: 1 }]]));
    const col1 = columns(layout)[1];
    const centre = (Math.min(...col1.map((x) => x.x)) + Math.max(...col1.map((x) => x.x + x.w))) / 2;
    expect(h.get('D')!.x + 20).toBe(centre);
    // Moved there, the layout's columns would have it with B and C.
    const moved = layout.map((x) => (x.id === 'D' ? { ...x, ...h.get('D')! } : x));
    expect(cells(moved).get('D')!.column).toBe(1);
  });

  it('puts a node asking for a row just above the one there now, or below the last', () => {
    const top = hints(layout, new Map([['C', { row: 0 }]]));
    expect(top.get('C')!.y).toBeLessThan(top.get('B')!.y);
    const into = hints(layout, new Map([['A', { column: 1, row: 1 }]]));
    expect(into.get('A')!.y).toBe(99); // just above C
    const last = hints(layout, new Map([['A', { column: 1, row: 5 }]]));
    expect(last.get('A')!.y).toBe(141); // below C
  });

  it('steps past the last column for a column further right', () => {
    const h = hints(layout, new Map([['A', { column: 4 }]]));
    expect(h.get('A')!.x).toBeGreaterThan(260 + 40);
    const moved = layout.map((x) => (x.id === 'A' ? { ...x, ...h.get('A')! } : x));
    expect(cells(moved).get('A')!.column).toBe(2); // after D: as far right as there is
  });

  it('keeps a dragged node in its column, where it was dragged across the flow', () => {
    const h = hints(layout, new Map(), new Map([['C', { y: -40 }]]));
    expect(h.get('C')).toEqual({ x: 120, y: -40 });
  });

  it('keeps a node asking for a column on its side of a node it is wired to', () => {
    // A is wired to B; A asks for D's column, past B: it's put just short of B.
    const wants = new Map([['A', { column: 2 }]]);
    const h = keepSides(layout, hints(layout, wants), [['A', 'B']], wants);
    expect(h.get('A')!.x + 40).toBeLessThanOrEqual(h.get('B')!.x);
    expect(h.get('A')!.x).toBeGreaterThanOrEqual(0); // not further back than it was
    // Not wired to anything in its way, it goes where it asked.
    const free = keepSides(layout, hints(layout, wants), [['B', 'D']], wants);
    expect(free.get('A')).toEqual(hints(layout, wants).get('A'));
  });

  it('moves only nodes that asked for a place', () => {
    const wants = new Map([['D', { column: 0 }]]);
    const h = keepSides(layout, hints(layout, wants), [['C', 'D']], wants);
    expect(h.get('C')).toEqual({ x: 120, y: 100 });
    expect(h.get('D')!.x).toBeGreaterThanOrEqual(120 + 60);
  });
});
