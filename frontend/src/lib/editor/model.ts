// The CAD model as the backend sends and takes it (internal/cad): mm,
// footprints y down, symbols y up.

/** A point. */
export interface Pt {
  x: number;
  y: number;
}

/** What a graphic is. */
export type GraphicKind = 'line' | 'rect' | 'circle' | 'arc' | 'poly' | 'text';

/** A drawn shape: see cad.Graphic for what its points are. */
export interface Graphic {
  kind: GraphicKind;
  layer: string;
  pts: Pt[];
  r?: number;
  width?: number;
  fill?: boolean;
  text?: string;
  size?: number;
  angle?: number;
}

/** A footprint pad. */
export interface Pad {
  number: string;
  kind: 'smd' | 'thru' | 'npth' | 'paste';
  shape: 'rect' | 'roundrect' | 'circle' | 'oval';
  x: number;
  y: number;
  w: number;
  h: number;
  drill?: number;
  noPaste?: boolean;
  via?: boolean;
}

/** SheetDiver's own outline line. */
export interface Line {
  layer: string;
  a: Pt;
  b: Pt;
  width: number;
}

/** SheetDiver's own filled mark. */
export interface Poly {
  layer: string;
  pts: Pt[];
}

/** An axis-aligned box. */
export interface Box {
  x0: number;
  y0: number;
  x1: number;
  y1: number;
}

/** A footprint. */
export interface FootprintModel {
  name: string;
  description: string;
  pads: Pad[];
  lines: Line[] | null;
  polys?: Poly[] | null;
  graphics?: Graphic[] | null;
  body: Box;
  courtyard: Box;
  from: string;
  mount: string;
}

/** Which side of the body a pin sticks out of. */
export type Side = 'left' | 'right' | 'top' | 'bottom';

/** A symbol pin: x, y is where a wire connects. */
export interface SymbolPin {
  number: string;
  name: string;
  type: string;
  side: Side;
  x: number;
  y: number;
  length: number;
  hidden?: boolean;
}

/** A symbol. */
export interface SymbolModel {
  name: string;
  reference: string;
  value: string;
  footprint?: string;
  datasheet?: string;
  description?: string;
  body: Box;
  pins: SymbolPin[];
  extra?: Line[] | null;
  graphics?: Graphic[] | null;
}

/** A point in 3D. */
export type V3 = [number, number, number];

/** A built solid. */
export interface Solid {
  part: string;
  color?: string;
  verts: V3[];
  faces: number[][];
}

/** A 3D model's shape: see cad.Shape3D. */
export interface Shape3D {
  kind: 'box' | 'cylinder' | 'solid';
  part: 'body' | 'lead' | 'pad' | 'mark';
  color?: string;
  min: V3;
  max: V3;
  x?: number;
  y?: number;
  r?: number;
  offset?: V3;
  solid?: Solid;
  name?: string;
}

/** A part's model for one package, to edit. */
export interface CadModel {
  part: string;
  package: string;
  symbol?: SymbolModel;
  footprint?: FootprintModel;
  shapes?: Shape3D[];
}

/** KiCad's electrical pin types. */
export const pinTypes = ['input', 'output', 'bidirectional', 'tri_state', 'passive', 'power_in', 'power_out', 'open_collector', 'no_connect', 'unspecified'];
