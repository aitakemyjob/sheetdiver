import { describe, expect, it } from 'vitest';
import { EditorController } from './controller';
import { FootprintDocument, SymbolDocument } from './documents';
import { fieldsOf } from './fields';
import { arcPoints, circleThrough } from './geometry';
import type { FootprintModel, SymbolModel } from './model';
import { Model3DController, draw } from './model3d';
import { pick } from './picking';

const footprint = (): FootprintModel => ({
  name: 'SOT-23',
  description: '',
  pads: [
    { number: '1', kind: 'smd', shape: 'roundrect', x: -1, y: -0.95, w: 1, h: 0.6 },
    { number: '2', kind: 'smd', shape: 'roundrect', x: -1, y: 0.95, w: 1, h: 0.6 },
  ],
  lines: [{ layer: 'silk', a: { x: -0.5, y: -1.6 }, b: { x: 0.5, y: -1.6 }, width: 0.12 }],
  polys: [{ layer: 'silk', pts: [{ x: -2, y: -1.5 }, { x: -1.7, y: -1.5 }, { x: -2, y: -1.2 }] }],
  body: { x0: -0.7, y0: -1.5, x1: 0.7, y1: 1.5 },
  courtyard: { x0: -2, y0: -2, x1: 2, y1: 2 },
  from: 'calculated',
  mount: 'smd',
});

const symbol = (): SymbolModel => ({
  name: 'U',
  reference: 'U',
  value: 'X',
  body: { x0: -5.08, y0: -2.54, x1: 5.08, y1: 2.54 },
  pins: [{ number: '1', name: 'IN', type: 'input', side: 'left', x: -7.62, y: 1.27, length: 2.54 }],
  graphics: [{ kind: 'text', layer: 'body', pts: [{ x: 0, y: 1 }], text: 'A', size: 1.27, angle: 0 }],
});

describe('the CAD editors', () => {
  it('turns a model into elements and back', () => {
    const d = FootprintDocument.of(footprint());
    expect(d.elements.map((e) => e.type)).toEqual(['graphic', 'graphic', 'pad', 'pad']);
    const m = d.model();
    expect(m.lines).toEqual([]);
    expect(m.graphics?.map((g) => g.kind)).toEqual(['line', 'poly']);
    expect(m.graphics?.[1].fill).toBe(true);
    // A symbol is edited y down and saved y up as it came.
    const s = SymbolDocument.of(symbol());
    const pin = s.elements.find((e) => e.type === 'pin');
    expect(pin?.type === 'pin' && pin.pin.y).toBe(-1.27);
    expect(s.model().pins[0].y).toBe(1.27);
    expect(s.model().body).toEqual(symbol().body);
    expect(s.model().graphics?.[0].pts[0]).toEqual({ x: 0, y: 1 });
  });

  it('draws a line click by click, snapped, and undoes it', () => {
    const ed = new EditorController(FootprintDocument.of(footprint()));
    ed.setGrid(0.5);
    ed.setLayer('fab');
    ed.setTool('line');
    ed.pointerDown({ x: 0.1, y: 0.1 }, false);
    ed.pointerMove({ x: 1.9, y: 0 });
    expect(ed.getSnapshot().preview).toHaveLength(1);
    ed.pointerDown({ x: 2.1, y: 0.2 }, false);
    const s = ed.getSnapshot();
    const last = s.doc.elements[s.doc.elements.length - 1];
    expect(last.type === 'graphic' && last.g).toMatchObject({ kind: 'line', layer: 'fab', pts: [{ x: 0, y: 0 }, { x: 2, y: 0 }] });
    expect(s.selection).toEqual([s.doc.elements.length - 1]);
    expect(s.dirty).toBe(true);
    ed.undo();
    expect(ed.getSnapshot().doc.elements).toHaveLength(4);
    ed.redo();
    expect(ed.getSnapshot().doc.elements).toHaveLength(5);
  });

  it('selects, moves and reshapes with the select tool', () => {
    const ed = new EditorController(FootprintDocument.of(footprint()));
    ed.tol = 0.05;
    ed.setGrid(0.05);
    // Press on pad 1, drag it 0.5 right.
    ed.pointerDown({ x: -1, y: -0.95 }, false);
    expect(ed.getSnapshot().selection).toEqual([2]);
    ed.pointerMove({ x: -0.75, y: -0.95 });
    ed.pointerMove({ x: -0.5, y: -0.95 });
    ed.pointerUp({ x: -0.5, y: -0.95 });
    const pad = ed.getSnapshot().doc.elements[2];
    expect(pad.type === 'pad' && pad.pad.x).toBeCloseTo(-0.5);
    // One undo takes the whole drag back.
    ed.undo();
    const back = ed.getSnapshot().doc.elements[2];
    expect(back.type === 'pad' && back.pad.x).toBe(-1);
    // Its corner handle sizes it about its centre.
    ed.select([2]);
    expect(pick({ x: -0.5, y: -0.65 }, { elements: ed.doc.elements, selection: [2], tol: 0.05 })).toEqual({ index: 2, handle: 0 });
    ed.pointerDown({ x: -0.5, y: -0.65 }, false);
    ed.pointerMove({ x: -0.25, y: -0.55 });
    ed.pointerUp({ x: -0.25, y: -0.55 });
    const sized = ed.getSnapshot().doc.elements[2];
    expect(sized.type === 'pad' && [sized.pad.w, sized.pad.h]).toEqual([1.5, 0.8]);
    // R turns it; Delete removes it.
    ed.key('r', false, false);
    const turned = ed.getSnapshot().doc.elements[2];
    expect(turned.type === 'pad' && [turned.pad.w, turned.pad.h]).toEqual([0.8, 1.5]);
    ed.key('Delete', false, false);
    expect(ed.getSnapshot().doc.elements.filter((e) => e.type === 'pad')).toHaveLength(1);
  });

  it('places pads numbered on and pins pointing into the body', () => {
    const ed = new EditorController(FootprintDocument.of(footprint()));
    ed.setTool('pad');
    ed.pointerDown({ x: 1, y: 0 }, false);
    const pad = ed.getSnapshot().doc.elements.at(-1);
    expect(pad?.type === 'pad' && [pad.pad.number, pad.pad.w]).toEqual(['3', 1]);

    const sym = new EditorController(SymbolDocument.of(symbol()));
    sym.setTool('pin');
    // On the body's right edge (x 5.08), the pin's end lands one length out.
    sym.pointerDown({ x: 5.08, y: 0 }, false);
    const pin = sym.getSnapshot().doc.elements.at(-1);
    expect(pin?.type === 'pin' && [pin.pin.side, pin.pin.x, pin.pin.number]).toEqual(['right', 7.62, '2']);
    // Its fields set its name and type.
    const fields = fieldsOf(pin!, ['body']);
    const named = fields.find((f) => f.label === 'Name')!.set(pin!, 'OUT');
    sym.update(sym.getSnapshot().doc.elements.length - 1, fields.find((f) => f.label === 'Type')!.set(named, 'output'));
    expect((sym.model() as SymbolModel).pins.at(-1)).toMatchObject({ name: 'OUT', type: 'output', side: 'right' });
  });

  it('draws arcs and polygons', () => {
    const ed = new EditorController(SymbolDocument.of(symbol()));
    ed.setTool('arc');
    for (const p of [{ x: -1, y: 0 }, { x: 0, y: -1 }, { x: 1, y: 0 }]) ed.pointerDown(p, false);
    const arc = ed.getSnapshot().doc.elements.at(-1);
    expect(arc?.type === 'graphic' && arc.g.kind).toBe('arc');
    expect(circleThrough({ x: -1, y: 0 }, { x: 0, y: -1 }, { x: 1, y: 0 })?.r).toBeCloseTo(1);
    expect(arcPoints({ x: -1, y: 0 }, { x: 0, y: -1 }, { x: 1, y: 0 }, 4)[2].y).toBeCloseTo(-1);
    ed.setTool('poly');
    for (const p of [{ x: 0, y: 0 }, { x: 2.54, y: 0 }, { x: 2.54, y: 2.54 }]) ed.pointerDown(p, false);
    ed.key('Enter', false, false);
    const poly = ed.getSnapshot().doc.elements.at(-1);
    expect(poly?.type === 'graphic' && poly.g.pts).toHaveLength(3);
    // Saved y up.
    expect(ed.model().graphics?.at(-1)?.pts[2]).toEqual({ x: 2.54, y: -2.54 });
  });

  it('edits a 3D model by its shapes', () => {
    const ed = new Model3DController([{ kind: 'box', part: 'body', min: [-1, -1, 0], max: [1, 1, 1] }]);
    expect(ed.getSnapshot().faces.length).toBe(3);
    ed.add('cylinder');
    expect(ed.getSnapshot().selected).toBe(1);
    ed.update(1, { ...ed.getSnapshot().shapes[1], r: 0.2, min: [0, 0, 1], max: [0, 0, 1.2] });
    expect(draw(ed.getSnapshot().shapes).some((f) => f.shape === 1)).toBe(true);
    ed.undo();
    expect(ed.getSnapshot().shapes[1].r).toBe(0.5);
    ed.remove(0);
    expect(ed.getSnapshot().shapes).toHaveLength(1);
  });
});
