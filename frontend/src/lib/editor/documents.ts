import type { Element } from './elements';
import type { Box, FootprintModel, Graphic, Line, Pt, SymbolModel } from './model';

// A document is what an editor works on: the elements of a symbol or a
// footprint in view coordinates (y down), made from the backend's model
// and turned back into it to save. Each kind is a document class; the
// editor knows only the interface.

/** What a kind of document knows: its layers, and how to save it. */
export interface EditDocument {
  /** symbol or footprint. */
  readonly kind: 'symbol' | 'footprint';
  /** The layers new graphics can go on. */
  readonly layers: string[];
  /** The elements, in drawing order. */
  readonly elements: Element[];
  /** The same document with other elements. */
  with(elements: Element[]): EditDocument;
  /** The model to save. */
  model(): SymbolModel | FootprintModel;
}

/** A line as a drawn graphic. */
function lineGraphic(l: Line, layer = l.layer): Graphic {
  return { kind: 'line', layer, pts: [l.a, l.b], width: l.width };
}

/** A footprint: its pads and everything drawn, SheetDiver's own outlines
 * becoming graphics like any other once it's edited. */
export class FootprintDocument implements EditDocument {
  readonly kind = 'footprint' as const;
  readonly layers = ['silk', 'fab', 'courtyard', 'copper'];

  constructor(
    private readonly base: FootprintModel,
    readonly elements: Element[],
  ) {}

  /** The document of a footprint. */
  static of(f: FootprintModel): FootprintDocument {
    const els: Element[] = [];
    for (const l of f.lines ?? []) els.push({ type: 'graphic', g: lineGraphic(l) });
    for (const p of f.polys ?? []) els.push({ type: 'graphic', g: { kind: 'poly', layer: p.layer, pts: p.pts, fill: true } });
    for (const g of f.graphics ?? []) els.push({ type: 'graphic', g });
    for (const pad of f.pads) els.push({ type: 'pad', pad });
    return new FootprintDocument(f, els);
  }

  with(elements: Element[]): FootprintDocument {
    return new FootprintDocument(this.base, elements);
  }

  model(): FootprintModel {
    const out: FootprintModel = { ...this.base, lines: [], polys: [], graphics: [], pads: [] };
    for (const e of this.elements) {
      if (e.type === 'graphic') out.graphics!.push(e.g);
      if (e.type === 'pad') out.pads.push(e.pad);
    }
    return out;
  }
}

/** A point flipped between y up and y down. */
const flip = (p: Pt): Pt => ({ x: p.x, y: p.y === 0 ? 0 : -p.y });

/** A box flipped between y up and y down. */
const flipBox = (b: Box): Box => ({ x0: b.x0, y0: b.y1 === 0 ? 0 : -b.y1, x1: b.x1, y1: b.y0 === 0 ? 0 : -b.y0 });

/** A graphic flipped between y up and y down (a text keeps its angle as seen). */
const flipGraphic = (g: Graphic): Graphic => ({ ...g, pts: g.pts.map(flip) });

/** Whether a box is empty: a symbol drawn without one. */
const emptyBox = (b: Box) => b.x0 === 0 && b.y0 === 0 && b.x1 === 0 && b.y1 === 0;

/** A symbol: its body, pins and drawing, turned y down to edit. */
export class SymbolDocument implements EditDocument {
  readonly kind = 'symbol' as const;
  readonly layers = ['body'];

  constructor(
    private readonly base: SymbolModel,
    readonly elements: Element[],
  ) {}

  /** The document of a symbol. */
  static of(s: SymbolModel): SymbolDocument {
    const els: Element[] = [];
    if (!emptyBox(s.body)) els.push({ type: 'body', box: flipBox(s.body) });
    for (const l of s.extra ?? []) els.push({ type: 'graphic', g: flipGraphic(lineGraphic(l, 'body')) });
    for (const g of s.graphics ?? []) els.push({ type: 'graphic', g: flipGraphic({ ...g, layer: 'body' }) });
    for (const pin of s.pins) els.push({ type: 'pin', pin: { ...pin, y: pin.y === 0 ? 0 : -pin.y } });
    return new SymbolDocument(s, els);
  }

  with(elements: Element[]): SymbolDocument {
    return new SymbolDocument(this.base, elements);
  }

  model(): SymbolModel {
    const out: SymbolModel = { ...this.base, body: { x0: 0, y0: 0, x1: 0, y1: 0 }, extra: [], graphics: [], pins: [] };
    for (const e of this.elements) {
      if (e.type === 'body') out.body = flipBox(e.box);
      if (e.type === 'graphic') out.graphics!.push(flipGraphic(e.g));
      if (e.type === 'pin') out.pins.push({ ...e.pin, y: e.pin.y === 0 ? 0 : -e.pin.y });
    }
    return out;
  }
}
