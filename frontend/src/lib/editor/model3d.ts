import { History } from './history';
import type { FootprintModel, Pt, Shape3D, Solid, V3 } from './model';

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
  if (s.part === 'mark') return [0.95, 0.95, 0.95];
  const named: Record<string, [number, number, number]> = { tan: [0.76, 0.66, 0.48], brown: [0.45, 0.3, 0.18], blue: [0.15, 0.25, 0.55], grey: [0.4, 0.4, 0.42], gray: [0.4, 0.4, 0.42] };
  return named[s.color ?? ''] ?? [0.12, 0.12, 0.13];
}

/** A face's unit normal (Newell's). */
function normal(s: Solid, f: number[]): V3 {
  const n: V3 = [0, 0, 0];
  f.forEach((vi, k) => {
    const a = s.verts[vi];
    const b = s.verts[f[(k + 1) % f.length]];
    n[0] += (a[1] - b[1]) * (a[2] + b[2]);
    n[1] += (a[2] - b[2]) * (a[0] + b[0]);
    n[2] += (a[0] - b[0]) * (a[1] + b[1]);
  });
  const nl = Math.hypot(...n) || 1;
  return n.map((x) => x / nl) as V3;
}

/** How far a point is out past a solid's face (negative: inside it). */
function outPast(s: Solid, f: number[], n: V3, p: V3): number {
  const o = s.verts[f[0]];
  return n[0] * (p[0] - o[0]) + n[1] * (p[1] - o[1]) + n[2] * (p[2] - o[2]);
}

/** A solid's bounding box. */
function bounds(s: Solid): [V3, V3] {
  const lo: V3 = [Infinity, Infinity, Infinity];
  const hi: V3 = [-Infinity, -Infinity, -Infinity];
  for (const v of s.verts) for (let k = 0; k < 3; k++) [lo[k], hi[k]] = [Math.min(lo[k], v[k]), Math.max(hi[k], v[k])];
  return [lo, hi];
}

/**
 * -1 when a solid sits under a body (out past a face of it the viewer
 * doesn't see), 1 when over it (out past one they see), 0 when it touches
 * no body.
 */
function onBody(s: Solid, bodies: Solid[], seen: (n: V3) => boolean): number {
  const [lo, hi] = bounds(s);
  let rank = 0;
  for (const b of bodies) {
    const [blo, bhi] = bounds(b);
    if ([0, 1, 2].some((k) => lo[k] > bhi[k] + 0.05 || blo[k] > hi[k] + 0.05)) continue;
    for (const f of b.faces) {
      const n = normal(b, f);
      if (!s.verts.some((v) => outPast(b, f, n, v) > 1e-6)) continue;
      if (seen(n)) return 1;
      rank = -1;
    }
  }
  return rank;
}

/** Whether a point is inside one of the (convex) bodies. */
function insideAny(p: V3, bodies: Solid[]): boolean {
  return bodies.some((b) => b.faces.every((f) => outPast(b, f, normal(b, f), p) < -1e-6));
}

/**
 * A face as drawn: its points, colour and the shape it belongs to (-1 for
 * the board and footprint under the model); a line is a stroke, not a fill.
 */
export interface Face {
  pts: Pt[];
  color: string;
  shape: number;
  line?: number;
}

/**
 * Where the view looks from, in degrees: yaw turns it round the board,
 * tilt is down from the horizon (90 straight down). It stays above the
 * board, where drawing far faces first holds.
 */
export interface Camera {
  yaw: number;
  tilt: number;
}

/** The view the backend draws: from above the front right. */
export const homeCamera: Camera = { yaw: -35, tilt: 55 };

/** Straight down onto the footprint. */
export const topCamera: Camera = { yaw: 0, tilt: 90 };

/** A camera turned by a drag, kept above the board. */
export function orbit(c: Camera, dYaw: number, dTilt: number): Camera {
  return { yaw: ((((c.yaw + dYaw + 180) % 360) + 360) % 360) - 180, tilt: Math.min(90, Math.max(5, c.tilt + dTilt)) };
}

/** A camera's projection: a point's place on screen and its depth, and whether a face's normal faces the viewer. */
export function projector(c: Camera): { project: (v: V3) => [Pt, number]; seen: (n: V3) => boolean } {
  const yaw = (c.yaw * Math.PI) / 180;
  const tilt = (c.tilt * Math.PI) / 180;
  const [ca, sa, cb, sb] = [Math.cos(yaw), Math.sin(yaw), Math.cos(tilt), Math.sin(tilt)];
  return {
    project: (v) => {
      const x1 = v[0] * ca - v[1] * sa;
      const y1 = v[0] * sa + v[1] * ca;
      return [{ x: x1, y: -(y1 * sb + v[2] * cb) }, y1 * cb - v[2] * sb];
    },
    seen: (n) => (n[0] * sa + n[1] * ca) * cb - n[2] * sb < -1e-9,
  };
}

/** A footprint point (y down) on the board. */
const onBoard = (p: Pt): V3 => [p.x, -p.y, 0];

/** A pad's outline, footprint coordinates. */
function padOutline(x: number, y: number, w: number, h: number, shape: string): Pt[] {
  if (shape !== 'circle' && shape !== 'oval') {
    return [
      { x: x - w / 2, y: y - h / 2 },
      { x: x + w / 2, y: y - h / 2 },
      { x: x + w / 2, y: y + h / 2 },
      { x: x - w / 2, y: y + h / 2 },
    ];
  }
  // A stadium: two half circles joined along the long side.
  const r = Math.min(w, h) / 2;
  const [dx, dy] = [w / 2 - r, h / 2 - r];
  const pts: Pt[] = [];
  for (let i = 0; i < 24; i++) {
    const a = (2 * Math.PI * i) / 24;
    const [cx, cy] = [Math.cos(a) >= 0 ? dx : -dx, Math.sin(a) >= 0 ? dy : -dy];
    pts.push({ x: x + cx + r * Math.cos(a), y: y + cy + r * Math.sin(a) });
  }
  return pts;
}

/** The board under a footprint and its copper, silkscreen and courtyard, flat at z 0. */
function footprintFaces(fp: FootprintModel, project: (v: V3) => [Pt, number]): Face[] {
  const flat = (pts: Pt[]) => pts.map((p) => project(onBoard(p))[0]);
  const c = fp.courtyard;
  const m = 0.5;
  const out: Face[] = [{ pts: flat(padOutline((c.x0 + c.x1) / 2, (c.y0 + c.y1) / 2, c.x1 - c.x0 + 2 * m, c.y1 - c.y0 + 2 * m, 'rect')), color: '#1d4a2e', shape: -1 }];
  for (const p of fp.pads) {
    if (p.kind === 'paste') continue;
    if (p.kind !== 'npth') out.push({ pts: flat(padOutline(p.x, p.y, p.w, p.h, p.shape)), color: '#c9a44c', shape: -1 });
    if (p.drill) out.push({ pts: flat(padOutline(p.x, p.y, p.drill, p.drill, 'circle')), color: '#111', shape: -1 });
  }
  for (const l of fp.lines ?? []) {
    if (l.layer === 'silk') out.push({ pts: flat([l.a, l.b]), color: '#eee', shape: -1, line: Math.max(l.width, 0.05) });
  }
  for (const p of fp.polys ?? []) {
    if (p.layer === 'silk') out.push({ pts: flat(p.pts), color: '#eee', shape: -1 });
  }
  out.push({
    pts: flat([
      { x: c.x0, y: c.y0 },
      { x: c.x1, y: c.y0 },
      { x: c.x1, y: c.y1 },
      { x: c.x0, y: c.y1 },
      { x: c.x0, y: c.y0 },
    ]),
    color: '#c040c0',
    shape: -1,
    line: 0.03,
  });
  return out;
}

/** The middle of the model and its footprint, and how far they reach from it: what the view frames. */
export function frameOf(shapes: Shape3D[], fp?: FootprintModel): { centre: V3; radius: number } {
  const pts: V3[] = shapes.flatMap((sh) => solidOf(sh).verts);
  if (fp) {
    const c = fp.courtyard;
    pts.push(onBoard({ x: c.x0, y: c.y0 }), onBoard({ x: c.x1, y: c.y1 }));
  }
  if (!pts.length) return { centre: [0, 0, 0], radius: 5 };
  const lo = [0, 1, 2].map((k) => Math.min(...pts.map((p) => p[k])));
  const hi = [0, 1, 2].map((k) => Math.max(...pts.map((p) => p[k])));
  const centre = [0, 1, 2].map((k) => (lo[k] + hi[k]) / 2) as V3;
  return { centre, radius: Math.max(0.5, Math.hypot(hi[0] - lo[0], hi[1] - lo[1], hi[2] - lo[2]) / 2) };
}

/**
 * The shapes drawn from a camera (the backend's view by default), far
 * faces first, over the footprint they sit on when given.
 */
export function draw(shapes: Shape3D[], camera: Camera = homeCamera, fp?: FootprintModel): Face[] {
  const ll = Math.hypot(-0.4, -0.6, 1);
  const light: V3 = [-0.4 / ll, -0.6 / ll, 1 / ll];
  const { project, seen } = projector(camera);
  const solids = shapes.map(solidOf);
  const bodies = solids.filter((s) => s.part === 'body' && s.verts.length);
  const faces: (Face & { depth: number; solid: number; rank: number })[] = [];
  solids.forEach((s, si) => {
    if (!s.verts.length) return;
    const solidDepth = s.verts.reduce((d, v) => d + project(v)[1], 0) / s.verts.length;
    // A lead, pad or mark goes over the body it sits on when it sticks out
    // of a face of it the viewer sees, under it otherwise.
    const rank = s.part === 'body' ? 0 : onBody(s, bodies, seen);
    const c = rgb(s);
    for (const f of s.faces) {
      const nv = normal(s, f);
      if (!seen(nv)) continue;
      // A terminal's faces inside the body are hidden by it.
      const mid = [0, 1, 2].map((k) => f.reduce((m, vi) => m + s.verts[vi][k], 0) / f.length) as V3;
      if (rank !== 0 && insideAny(mid, bodies)) continue;
      const proj = f.map((vi) => project(s.verts[vi]));
      const k = 0.5 + 0.5 * Math.max(0, nv[0] * light[0] + nv[1] * light[1] + nv[2] * light[2]);
      const hex = (x: number) => Math.round(255 * x * k).toString(16).padStart(2, '0');
      faces.push({ pts: proj.map((p) => p[0]), depth: proj.reduce((d, p) => d + p[1], 0) / proj.length, solid: solidDepth, color: `#${hex(c[0])}${hex(c[1])}${hex(c[2])}`, shape: si, rank });
    }
  });
  faces.sort((a, b) => (a.rank !== b.rank ? a.rank - b.rank : a.solid !== b.solid ? b.solid - a.solid : b.depth - a.depth));
  // The board is under everything, seen only from above.
  return [...(fp ? footprintFaces(fp, project) : []), ...faces.map(({ pts, color, shape }) => ({ pts, color, shape }))];
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
