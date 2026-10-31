import type { EditDocument } from './documents';
import type { Element } from './elements';
import { History } from './history';
import type { Box, FootprintModel, Pt, SymbolModel } from './model';
import { snap } from './picking';
import { boxOfElements, createTool, toolsFor, type Tool, type ToolHost, type ToolName } from './tools';

// The editor: the document, its selection and undo history, the tool in
// hand and the view. React reads its snapshot and forwards the pointer
// and keys; everything the editor does is here.

/** What the editor shows. */
export interface EditorSnapshot {
  doc: EditDocument;
  selection: number[];
  tool: ToolName;
  tools: ToolName[];
  layer: string;
  grid: number;
  /** The view box, mm. */
  view: Box;
  preview: Element[];
  marquee: Box | null;
  dirty: boolean;
  canUndo: boolean;
  canRedo: boolean;
  hint: string;
}

/** What a snapshot of the document for undo holds. */
interface Saved {
  elements: Element[];
  selection: number[];
}

/** The grids offered, mm; a symbol's is KiCad's 1.27 or 2.54 mil grid. */
export const grids: Record<EditDocument['kind'], number[]> = { footprint: [0, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 1.27], symbol: [0.635, 1.27, 2.54] };

/** Edits one symbol or footprint. */
export class EditorController implements ToolHost {
  private snap$: EditorSnapshot;
  private readonly history = new History<Saved>();
  private readonly listeners = new Set<() => void>();
  private toolObj: Tool = createTool('select');
  /** The pick tolerance, mm: set from the zoom by the canvas. */
  tol = 0.1;

  constructor(doc: EditDocument) {
    this.snap$ = {
      doc,
      selection: [],
      tool: 'select',
      tools: toolsFor[doc.kind],
      layer: doc.layers[0],
      grid: doc.kind === 'symbol' ? 1.27 : 0.05,
      view: EditorController.fit(doc),
      preview: [],
      marquee: null,
      dirty: false,
      canUndo: false,
      canRedo: false,
      hint: this.toolObj.hint(),
    };
  }

  /** A view box round a document's drawing, with a margin. */
  static fit(doc: EditDocument): Box {
    const b = boxOfElements(doc.elements) ?? { x0: -5, y0: -5, x1: 5, y1: 5 };
    const m = Math.max(1, 0.15 * Math.max(b.x1 - b.x0, b.y1 - b.y0));
    return { x0: b.x0 - m, y0: b.y0 - m, x1: b.x1 + m, y1: b.y1 + m };
  }

  // The store React subscribes to.
  subscribe = (fn: () => void): (() => void) => {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  };

  getSnapshot = (): EditorSnapshot => this.snap$;

  /** Updates the snapshot and tells React. */
  private set(patch: Partial<EditorSnapshot>): void {
    this.snap$ = { ...this.snap$, ...patch, canUndo: this.history.canUndo, canRedo: this.history.canRedo };
    this.listeners.forEach((fn) => fn());
  }

  // ToolHost.
  get doc(): EditDocument {
    return this.snap$.doc;
  }

  get selection(): number[] {
    return this.snap$.selection;
  }

  get layer(): string {
    return this.snap$.layer;
  }

  get grid(): number {
    return this.snap$.grid;
  }

  snap(p: Pt, skip: Set<number> = new Set()): Pt {
    return snap(p, { elements: this.doc.elements, skip, grid: this.grid, tol: this.tol });
  }

  change(elements: Element[], selection: number[], record: boolean): void {
    if (record) this.history.push({ elements: this.doc.elements, selection: this.selection });
    this.set({ doc: this.doc.with(elements), selection, dirty: true });
  }

  select(selection: number[]): void {
    this.set({ selection });
  }

  preview(elements: Element[], marquee: Box | null = this.snap$.marquee): void {
    this.set({ preview: elements, marquee });
  }

  // What the UI does.
  setTool(name: ToolName): void {
    this.toolObj = createTool(name);
    this.set({ tool: name, preview: [], marquee: null, hint: this.toolObj.hint() });
  }

  setLayer(layer: string): void {
    this.set({ layer });
  }

  setGrid(grid: number): void {
    this.set({ grid });
  }

  pointerDown(p: Pt, additive: boolean): void {
    this.toolObj.down(this, p, additive);
  }

  pointerMove(p: Pt): void {
    this.toolObj.move(this, p);
  }

  pointerUp(p: Pt): void {
    this.toolObj.up(this, p);
  }

  /** A key pressed; true when the editor used it. */
  key(key: string, ctrl: boolean, shift: boolean): boolean {
    if (ctrl && (key === 'z' || key === 'Z')) {
      if (shift) this.redo();
      else this.undo();
      return true;
    }
    if (ctrl && (key === 'y' || key === 'Y')) {
      this.redo();
      return true;
    }
    if (ctrl) return false;
    return this.toolObj.key(this, key);
  }

  /** Replaces one element, as the properties panel edits it. */
  update(index: number, e: Element): void {
    const els = this.doc.elements.slice();
    els[index] = e;
    this.change(els, this.selection, true);
  }

  /** Removes what's selected. */
  removeSelected(): void {
    this.key('Delete', false, false);
  }

  undo(): void {
    const prev = this.history.undo({ elements: this.doc.elements, selection: this.selection });
    if (prev) this.set({ doc: this.doc.with(prev.elements), selection: prev.selection, dirty: true });
  }

  redo(): void {
    const next = this.history.redo({ elements: this.doc.elements, selection: this.selection });
    if (next) this.set({ doc: this.doc.with(next.elements), selection: next.selection, dirty: true });
  }

  /** Zooms by a factor about a point. */
  zoomAt(p: Pt, factor: number): void {
    const v = this.snap$.view;
    const f = Math.min(Math.max(factor, 0.2), 5);
    this.set({ view: { x0: p.x - (p.x - v.x0) * f, y0: p.y - (p.y - v.y0) * f, x1: p.x + (v.x1 - p.x) * f, y1: p.y + (v.y1 - p.y) * f } });
  }

  /** Moves the view by mm. */
  panBy(dx: number, dy: number): void {
    const v = this.snap$.view;
    this.set({ view: { x0: v.x0 - dx, y0: v.y0 - dy, x1: v.x1 - dx, y1: v.y1 - dy } });
  }

  /** Fits the view round the drawing. */
  fitView(): void {
    this.set({ view: EditorController.fit(this.doc) });
  }

  /** The model to save. */
  model(): SymbolModel | FootprintModel {
    return this.doc.model();
  }

  /** Marks what's shown as saved. */
  saved(doc: EditDocument): void {
    this.set({ doc, dirty: false, selection: this.selection.filter((i) => i < doc.elements.length) });
  }
}
