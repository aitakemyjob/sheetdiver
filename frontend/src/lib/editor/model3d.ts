import { History } from './history';
import type { Shape3D, Solid, V3 } from './model';

// The 3D model's editor: shapes (boxes, cylinders, solids as built) built
// into solids and drawn as an isometric view the way the backend draws
// it, so changes show at once; a face picked selects its shape.

/** A box between two corners given in footprint coordinates (y down), stored y up. */
function box(part: string, a: V3, b: V3): Solid {
  const [x0, x1] = [Math.min(a[0], b[0]), Math.max(a[0], b[0])];
  const [ya, yb] = [Math.min(-a[1], -b[1]), Math.max(-a[1], -b[1])];
  const [z0, z1] = [Math.min(a[2], b[2]), Math.max(a[2], b[2])];
  return {
    part,
    verts: [
      [x0, ya, z0],
      [x1, ya, z0],
      [x1, yb, z0],
      [x0, yb, z0],
      [x0, ya, z1],
      [x1, ya, z1],
      [x1, yb, z1],
      [x0, yb, z1],
    ],
    faces: [
      [0, 3, 2, 1],
      [4, 5, 6, 7],
      [0, 1, 5, 4],
      [1, 2, 6, 5],
      [2, 3, 7, 6],
      [3, 0, 4, 7],
    ],
  };
}

/** An upright n-sided prism centred on (x, y), footprint coordinates. */
function prism(part: string, x: number, y: number, r: number, z0: number, z1: number, n = 24): Solid {
  const verts: V3[] = [];
  for (const z of [z0, z1]) {
    for (let i = 0; i < n; i++) {
      const a = (2 * Math.PI * (i + 0.5)) / n;
      verts.push([x + r * Math.cos(a), -y + r * Math.sin(a), z]);
    }
  }
  const faces: number[][] = [Array.from({ length: n }, (_, i) => n - 1 - i), Array.from({ length: n }, (_, i) => n + i)];
  for (let i = 0; i < n; i++) faces.push([i, (i + 1) % n, n + ((i + 1) % n), n + i]);
  return { part, verts, faces };
}

/** A shape's solid. */
export function solidOf(sh: Shape3D): Solid {
  switch (sh.kind) {
    case 'box':
      return { ...box(sh.part, sh.min, sh.max), color: sh.color };
    case 'cylinder':
      return { ...prism(sh.part, sh.x ?? 0, sh.y ?? 0, sh.r ?? 0.1, sh.min[2], sh.max[2]), color: sh.color };
  }
  const o = sh.offset ?? [0, 0, 0];
  const s = sh.solid ?? { part: sh.part, verts: [], faces: [] };
  return { part: sh.part, color: sh.color, faces: s.faces, verts: s.verts.map((v) => [v[0] + o[0], v[1] - o[1], v[2] + o[2]] as V3) };
}

/** A solid's colour, as the backend colours it. */
function rgb(s: Solid): [number, number, number] {
  if (s.part === 'lead' || s.part === 'pad') return [0.82, 0.82, 0.84];
  if (s.part === 'mark') return [0.55, 0.55, 0.55];
  const named: Record<string, [number, number, number]> = { tan: [0.76, 0.66, 0.48], brown: [0.45, 0.3, 0.18], blue: [0.15, 0.25, 0.55], grey: [0.4, 0.4, 0.42], gray: [0.4, 0.4, 0.42] };
  return named[s.color ?? ''] ?? [0.12, 0.12, 0.13];
}

/** A face as drawn: its points, colour and the shape it belongs to. */
export interface Face {
  pts: { x: number; y: number }[];
  color: string;
  shape: number;
}

/** The shapes drawn isometric from above the front right, far faces first. */
export function draw(shapes: Shape3D[]): Face[] {
  const yaw = (-35 * Math.PI) / 180;
  const tilt = (55 * Math.PI) / 180;
  const [ca, sa, cb, sb] = [Math.cos(yaw), Math.sin(yaw), Math.cos(tilt), Math.sin(tilt)];
  const ll = Math.hypot(-0.4, -0.6, 1);
  const light: V3 = [-0.4 / ll, -0.6 / ll, 1 / ll];
  const project = (v: V3): [{ x: number; y: number }, number] => {
    const x1 = v[0] * ca - v[1] * sa;
    const y1 = v[0] * sa + v[1] * ca;
    return [{ x: x1, y: -(y1 * sb + v[2] * cb) }, y1 * cb - v[2] * sb];
  };
  const faces: (Face & { depth: number; solid: number })[] = [];
  shapes.forEach((sh, si) => {
    const s = solidOf(sh);
    if (!s.verts.length) return;
    let solidDepth = s.verts.reduce((d, v) => d + project(v)[1], 0) / s.verts.length;
    if (s.part === 'mark') solidDepth -= 1e3;
    if (s.part === 'pad') solidDepth += 1e3;
    const c = rgb(s);
    for (const f of s.faces) {
      // Newell's normal.
      const n: V3 = [0, 0, 0];
      f.forEach((vi, k) => {
        const a = s.verts[vi];
        const b = s.verts[f[(k + 1) % f.length]];
        n[0] += (a[1] - b[1]) * (a[2] + b[2]);
        n[1] += (a[2] - b[2]) * (a[0] + b[0]);
        n[2] += (a[0] - b[0]) * (a[1] + b[1]);
      });
      const nl = Math.hypot(...n) || 1;
      const nv = n.map((x) => x / nl) as V3;
      if ((nv[0] * sa + nv[1] * ca) * cb - nv[2] * sb >= -1e-9) continue;
      const proj = f.map((vi) => project(s.verts[vi]));
      const k = 0.5 + 0.5 * Math.max(0, nv[0] * light[0] + nv[1] * light[1] + nv[2] * light[2]);
      const hex = (x: number) => Math.round(255 * x * k).toString(16).padStart(2, '0');
      faces.push({ pts: proj.map((p) => p[0]), depth: proj.reduce((d, p) => d + p[1], 0) / proj.length, solid: solidDepth, color: `#${hex(c[0])}${hex(c[1])}${hex(c[2])}`, shape: si });
    }
  });
  faces.sort((a, b) => (a.solid !== b.solid ? b.solid - a.solid : b.depth - a.depth));
  return faces.map(({ pts, color, shape }) => ({ pts, color, shape }));
}

/** A view box round faces. */
export function viewOf(faces: Face[]): { x0: number; y0: number; x1: number; y1: number } {
  const pts = faces.flatMap((f) => f.pts);
  if (!pts.length) return { x0: -5, y0: -5, x1: 5, y1: 5 };
  const xs = pts.map((p) => p.x);
  const ys = pts.map((p) => p.y);
  const m = 0.1 * Math.max(Math.max(...xs) - Math.min(...xs), Math.max(...ys) - Math.min(...ys), 1);
  return { x0: Math.min(...xs) - m, y0: Math.min(...ys) - m, x1: Math.max(...xs) + m, y1: Math.max(...ys) + m };
}

/** What a shape can be given, by field: a factory of fields by shape kind. */
export interface ShapeField {
  label: string;
  kind: 'number' | 'select';
  options?: string[];
  get(s: Shape3D): number | string;
  set(s: Shape3D, v: number | string): Shape3D;
}

/** A field of a coordinate of min, max or offset. */
function v3Field(label: string, key: 'min' | 'max' | 'offset', axis: 0 | 1 | 2): ShapeField {
  return {
    label,
    kind: 'number',
    get: (s) => (s[key] ?? [0, 0, 0])[axis],
    set: (s, v) => {
      const next = [...(s[key] ?? [0, 0, 0])] as V3;
      next[axis] = Number(v);
      return { ...s, [key]: next };
    },
  };
}

/** A field of a number of a shape. */
function numField(label: string, key: 'x' | 'y' | 'r'): ShapeField {
  return { label, kind: 'number', get: (s) => s[key] ?? 0, set: (s, v) => ({ ...s, [key]: Number(v) }) };
}

/** Every shape's part and colour. */
const common: ShapeField[] = [
  { label: 'Name', kind: 'select', options: [], get: (s) => s.name ?? '', set: (s, v) => ({ ...s, name: String(v) }) },
  { label: 'Part', kind: 'select', options: ['body', 'lead', 'pad', 'mark'], get: (s) => s.part, set: (s, v) => ({ ...s, part: v as Shape3D['part'] }) },
  { label: 'Colour', kind: 'select', options: ['', 'black', 'grey', 'tan', 'brown', 'blue'], get: (s) => s.color ?? '', set: (s, v) => ({ ...s, color: String(v) || undefined }) },
];

/** The fields by shape kind. */
const shapeFields: Record<Shape3D['kind'], ShapeField[]> = {
  box: [v3Field('x from', 'min', 0), v3Field('x to', 'max', 0), v3Field('y from', 'min', 1), v3Field('y to', 'max', 1), v3Field('z from', 'min', 2), v3Field('z to', 'max', 2)],
  cylinder: [numField('Centre x', 'x'), numField('Centre y', 'y'), numField('Radius', 'r'), v3Field('z from', 'min', 2), v3Field('z to', 'max', 2)],
  solid: [v3Field('Move x', 'offset', 0), v3Field('Move y', 'offset', 1), v3Field('Move z', 'offset', 2)],
};

/** The fields of a shape (its name edited as text). */
export function shapeFieldsOf(s: Shape3D): ShapeField[] {
  return [...common.slice(1), ...shapeFields[s.kind]];
}

/** A new shape of a kind, on the board under the model's middle. */
export function newShape(kind: 'box' | 'cylinder', n: number): Shape3D {
  return kind === 'box' ? { kind, part: 'body', name: `box ${n}`, min: [-0.5, -0.5, 0], max: [0.5, 0.5, 1] } : { kind, part: 'body', name: `cylinder ${n}`, x: 0, y: 0, r: 0.5, min: [0, 0, 0], max: [0, 0, 1] };
}

/** What the 3D editor shows. */
export interface Model3DSnapshot {
  shapes: Shape3D[];
  selected: number;
  faces: Face[];
  dirty: boolean;
  canUndo: boolean;
  canRedo: boolean;
}

/** Edits a 3D model's shapes, with undo. */
export class Model3DController {
  private snap$: Model3DSnapshot;
  private readonly history = new History<Shape3D[]>();
  private readonly listeners = new Set<() => void>();

  constructor(shapes: Shape3D[]) {
    this.snap$ = { shapes, selected: -1, faces: draw(shapes), dirty: false, canUndo: false, canRedo: false };
  }

  subscribe = (fn: () => void): (() => void) => {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  };

  getSnapshot = (): Model3DSnapshot => this.snap$;

  /** Sets the shapes (remembering the old ones) and redraws. */
  private setShapes(shapes: Shape3D[], selected: number, record = true): void {
    if (record) this.history.push(this.snap$.shapes);
    this.snap$ = { shapes, selected, faces: draw(shapes), dirty: true, canUndo: this.history.canUndo, canRedo: this.history.canRedo };
    this.listeners.forEach((fn) => fn());
  }

  select(i: number): void {
    this.snap$ = { ...this.snap$, selected: i };
    this.listeners.forEach((fn) => fn());
  }

  update(i: number, s: Shape3D): void {
    this.setShapes(
      this.snap$.shapes.map((x, j) => (j === i ? s : x)),
      i,
    );
  }

  add(kind: 'box' | 'cylinder'): void {
    const shapes = [...this.snap$.shapes, newShape(kind, this.snap$.shapes.length + 1)];
    this.setShapes(shapes, shapes.length - 1);
  }

  duplicate(i: number): void {
    const s = this.snap$.shapes[i];
    if (!s) return;
    const shapes = [...this.snap$.shapes, { ...s, name: `${s.name ?? s.kind} copy` }];
    this.setShapes(shapes, shapes.length - 1);
  }

  remove(i: number): void {
    this.setShapes(
      this.snap$.shapes.filter((_, j) => j !== i),
      -1,
    );
  }

  undo(): void {
    const prev = this.history.undo(this.snap$.shapes);
    if (prev) this.setShapes(prev, -1, false);
  }

  redo(): void {
    const next = this.history.redo(this.snap$.shapes);
    if (next) this.setShapes(next, -1, false);
  }

  /** Marks the shapes as saved. */
  saved(): void {
    this.snap$ = { ...this.snap$, dirty: false };
    this.listeners.forEach((fn) => fn());
  }
}
