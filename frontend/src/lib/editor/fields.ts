import type { Element } from './elements';
import { pinTypes, type Graphic, type Pad, type SymbolPin } from './model';

// The properties panel's fields for what's selected: a factory of field
// lists by element (and graphic) kind. Each field reads its value from an
// element and writes it back as a new element.

/** A field of the properties panel. */
export interface Field {
  label: string;
  kind: 'number' | 'text' | 'select' | 'bool';
  options?: string[];
  get(e: Element): number | string | boolean;
  set(e: Element, v: number | string | boolean): Element;
}

/** A field of a graphic. */
function gField(label: string, kind: Field['kind'], get: (g: Graphic) => number | string | boolean, set: (g: Graphic, v: never) => Graphic, options?: string[]): Field {
  return {
    label,
    kind,
    options,
    get: (e) => (e.type === 'graphic' ? get(e.g) : ''),
    set: (e, v) => (e.type === 'graphic' ? { ...e, g: set(e.g, v as never) } : e),
  };
}

/** A field of a point of a graphic. */
function ptField(label: string, i: number, axis: 'x' | 'y'): Field {
  return gField(label, 'number', (g) => g.pts[i]?.[axis] ?? 0, (g, v: number) => ({ ...g, pts: g.pts.map((p, j) => (j === i ? { ...p, [axis]: v } : p)) }));
}

/** A field of a pad. */
function padField(label: string, kind: Field['kind'], key: keyof Pad, options?: string[]): Field {
  return {
    label,
    kind,
    options,
    get: (e) => (e.type === 'pad' ? ((e.pad[key] as number | string | boolean | undefined) ?? (kind === 'bool' ? false : kind === 'number' ? 0 : '')) : ''),
    set: (e, v) => (e.type === 'pad' ? { ...e, pad: { ...e.pad, [key]: v } } : e),
  };
}

/** A field of a pin. */
function pinField(label: string, kind: Field['kind'], key: keyof SymbolPin, options?: string[]): Field {
  return {
    label,
    kind,
    options,
    get: (e) => (e.type === 'pin' ? ((e.pin[key] as number | string | boolean | undefined) ?? (kind === 'bool' ? false : '')) : ''),
    set: (e, v) => (e.type === 'pin' ? { ...e, pin: { ...e.pin, [key]: v } } : e),
  };
}

/** A graphic's layer, for the layers a document has. */
const layerField = (layers: string[]) => gField('Layer', 'select', (g) => g.layer, (g, v: string) => ({ ...g, layer: v }), layers);

/** Every graphic's stroke. */
const strokeFields = [gField('Width', 'number', (g) => g.width ?? 0, (g, v: number) => ({ ...g, width: v }))];

/** The fields by graphic kind. */
const graphicFields: Record<Graphic['kind'], Field[]> = {
  line: [ptField('Start x', 0, 'x'), ptField('Start y', 0, 'y'), ptField('End x', 1, 'x'), ptField('End y', 1, 'y'), ...strokeFields],
  rect: [ptField('Corner x', 0, 'x'), ptField('Corner y', 0, 'y'), ptField('Opposite x', 1, 'x'), ptField('Opposite y', 1, 'y'), ...strokeFields, gField('Filled', 'bool', (g) => !!g.fill, (g, v: boolean) => ({ ...g, fill: v }))],
  circle: [ptField('Centre x', 0, 'x'), ptField('Centre y', 0, 'y'), gField('Radius', 'number', (g) => g.r ?? 0, (g, v: number) => ({ ...g, r: v })), ...strokeFields, gField('Filled', 'bool', (g) => !!g.fill, (g, v: boolean) => ({ ...g, fill: v }))],
  arc: [ptField('Start x', 0, 'x'), ptField('Start y', 0, 'y'), ptField('Through x', 1, 'x'), ptField('Through y', 1, 'y'), ptField('End x', 2, 'x'), ptField('End y', 2, 'y'), ...strokeFields],
  poly: [...strokeFields, gField('Filled', 'bool', (g) => !!g.fill, (g, v: boolean) => ({ ...g, fill: v }))],
  text: [
    gField('Text', 'text', (g) => g.text ?? '', (g, v: string) => ({ ...g, text: v })),
    ptField('x', 0, 'x'),
    ptField('y', 0, 'y'),
    gField('Height', 'number', (g) => g.size ?? 1, (g, v: number) => ({ ...g, size: v, width: v * 0.15 })),
    gField('Angle °', 'number', (g) => g.angle ?? 0, (g, v: number) => ({ ...g, angle: v })),
  ],
};

/** The fields of a pad. */
const padFields: Field[] = [
  padField('Number', 'text', 'number'),
  padField('Kind', 'select', 'kind', ['smd', 'thru', 'npth', 'paste']),
  padField('Shape', 'select', 'shape', ['rect', 'roundrect', 'circle', 'oval']),
  padField('x', 'number', 'x'),
  padField('y', 'number', 'y'),
  padField('Width', 'number', 'w'),
  padField('Height', 'number', 'h'),
  padField('Drill', 'number', 'drill'),
  padField('No paste', 'bool', 'noPaste'),
];

/** The fields of a pin. */
const pinFields: Field[] = [
  pinField('Number', 'text', 'number'),
  pinField('Name', 'text', 'name'),
  pinField('Type', 'select', 'type', pinTypes),
  pinField('Side', 'select', 'side', ['left', 'right', 'top', 'bottom']),
  pinField('x', 'number', 'x'),
  pinField('y', 'number', 'y'),
  pinField('Length', 'number', 'length'),
  pinField('Hidden', 'bool', 'hidden'),
];

/** The fields of the body box. */
const bodyFields: Field[] = (['x0', 'y0', 'x1', 'y1'] as const).map((k) => ({
  label: { x0: 'Left', y0: 'Top', x1: 'Right', y1: 'Bottom' }[k],
  kind: 'number' as const,
  get: (e: Element) => (e.type === 'body' ? e.box[k] : 0),
  set: (e: Element, v: number | string | boolean) => (e.type === 'body' ? { ...e, box: { ...e.box, [k]: v as number } } : e),
}));

/** The fields of an element, for the layers its document has. */
export function fieldsOf(e: Element, layers: string[]): Field[] {
  switch (e.type) {
    case 'graphic':
      return layers.length > 1 ? [layerField(layers), ...graphicFields[e.g.kind]] : graphicFields[e.g.kind];
    case 'pad':
      return padFields;
    case 'pin':
      return pinFields;
    case 'body':
      return bodyFields;
  }
}

/** What an element is, in words. */
export function describe(e: Element): string {
  switch (e.type) {
    case 'graphic':
      return { line: 'Line', rect: 'Rectangle', circle: 'Circle', arc: 'Arc', poly: 'Polygon', text: 'Text' }[e.g.kind] + ` on ${e.g.layer}`;
    case 'pad':
      return `Pad ${e.pad.number}`;
    case 'pin':
      return `Pin ${e.pin.number} ${e.pin.name}`;
    case 'body':
      return 'Body';
  }
}
