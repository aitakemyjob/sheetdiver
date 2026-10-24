// Pictures of the drawn block diagram: the SVG the app shows, standing
// alone (its theme's variables resolved, at full size, cropped to the
// whole drawing or one group's frame), and, for the agent, that picture
// as a PNG, in answer to a request from the backend (core.Snapshot).

import type { BlockDrawing, Rect } from '../blockDiagram';
import type { BusEvent } from '../types';

/** The backend asking for a picture (core.SnapshotRequest). */
export interface SnapshotRequest {
  id: string;
  project: string;
  group?: string;
}

/** The answer: a PNG (base64), or why there's none (core.Snapshot). */
export interface Snapshot {
  id: string;
  png?: string;
  width: number;
  height: number;
  problem?: string;
}

/** Turns a standalone SVG into a PNG of the given size, base64. */
export type Rasterize = (svg: string, width: number, height: number) => Promise<string>;

/** A theme variable's value (`--bg`), for an SVG that stands alone. */
export type Resolve = (name: string) => string;

/** The longest side of a picture for the agent: larger is scaled down by the model anyway. */
export const MAX_EDGE = 1568;
/** The most a picture is scaled up, so a small group is still sharp. */
const MAX_SCALE = 2;
/** Room round a group's frame in its picture. */
const MARGIN = 10;

/**
 * The SVG the app shows as a standalone document: the whole drawing at
 * full size (whatever the view's zoom and pan), shown through `crop`,
 * `width` by `height` pixels, its theme's variables resolved, over a
 * background if one is given.
 */
export function standaloneSVG(el: SVGSVGElement, crop: Rect, width: number, height: number, resolve: Resolve, background?: string): string {
  const copy = el.cloneNode(true) as SVGSVGElement;
  copy.setAttribute('xmlns', 'http://www.w3.org/2000/svg');
  copy.setAttribute('width', String(width));
  copy.setAttribute('height', String(height));
  copy.setAttribute('viewBox', `${crop.x} ${crop.y} ${crop.w} ${crop.h}`);
  // The shown SVG's style sizes it for the page; only its font is kept.
  const font = el.style.fontFamily;
  copy.removeAttribute('style');
  copy.setAttribute('font-family', font ? `${font}, sans-serif` : 'sans-serif');
  copy.querySelector('g[data-view]')?.removeAttribute('transform');
  if (background) {
    const bg = document.createElementNS('http://www.w3.org/2000/svg', 'rect');
    for (const [k, v] of Object.entries({ x: crop.x, y: crop.y, width: crop.w, height: crop.h, fill: background })) bg.setAttribute(k, String(v));
    copy.insertBefore(bg, copy.firstChild);
  }
  // A value goes inside an attribute: its quotes (a font stack's) become single ones.
  return copy.outerHTML.replace(/var\((--[\w-]+)\)/g, (_, v: string) => (resolve(v) || 'sans-serif').replace(/"/g, "'"));
}

/** Where a picture of the drawing looks: all of it, or one group's frame; or why it can't. */
export function cropOf(d: BlockDrawing, group?: string): Rect | string {
  if (!group) return { x: 0, y: 0, w: d.width, h: d.height };
  const f = d.frames.find((x) => x.group.name.toLowerCase() === group.toLowerCase());
  if (f) return { x: f.x - MARGIN, y: f.y - MARGIN, w: f.w + 2 * MARGIN, h: f.h + 2 * MARGIN };
  const names = d.frames.map((x) => x.group.name);
  return names.length ? `no group "${group}" on the diagram; its groups are ${names.join(', ')}` : `the diagram has no groups, so there's no "${group}"`;
}

/** A picture's size in pixels: the crop scaled to fit MAX_EDGE, at most twice its size. */
export function pictureSize(crop: Rect): { width: number; height: number } {
  const k = Math.min(MAX_SCALE, MAX_EDGE / Math.max(crop.w, crop.h, 1));
  return { width: Math.max(1, Math.round(crop.w * k)), height: Math.max(1, Math.round(crop.h * k)) };
}

/** What a snapshot responder draws from: the project shown, its drawing and its SVG. */
export interface Shown {
  project: string;
  drawing: BlockDrawing;
  svg: SVGSVGElement;
}

/**
 * Answers the backend's requests for a picture of the block diagram the
 * app shows; a request for another project isn't its to answer.
 */
export class SnapshotResponder {
  constructor(
    private readonly rasterize: Rasterize,
    private readonly resolve: Resolve,
  ) {}

  /** The answer to a request, or undefined when it's for another project. */
  async answer(req: SnapshotRequest, shown: Shown): Promise<Snapshot | undefined> {
    if (req.project.toLowerCase() !== shown.project.toLowerCase()) return undefined;
    const crop = cropOf(shown.drawing, req.group);
    if (typeof crop === 'string') return { id: req.id, width: 0, height: 0, problem: crop };
    const { width, height } = pictureSize(crop);
    try {
      const svg = standaloneSVG(shown.svg, crop, width, height, this.resolve, this.resolve('--bg') || '#ffffff');
      return { id: req.id, png: await this.rasterize(svg, width, height), width, height };
    } catch (e) {
      return { id: req.id, width, height, problem: e instanceof Error ? e.message : String(e) };
    }
  }
}

/** Draws an SVG on a canvas and reads it back as PNG, in the browser. */
export const canvasRasterize: Rasterize = (svg, width, height) =>
  new Promise((resolve, reject) => {
    const img = new Image();
    img.onload = () => {
      const canvas = document.createElement('canvas');
      canvas.width = width;
      canvas.height = height;
      const g = canvas.getContext('2d');
      if (!g) return reject(new Error('no canvas to draw the picture on'));
      g.drawImage(img, 0, 0, width, height);
      resolve(canvas.toDataURL('image/png').replace(/^data:image\/png;base64,/, ''));
    };
    img.onerror = () => reject(new Error('the drawing could not be turned into a picture'));
    img.src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
  });

/** The document's theme variables, resolved. */
export const documentResolve: Resolve = (name) => getComputedStyle(document.documentElement).getPropertyValue(name).trim();

/**
 * Answers each picture request on the backend's events with what's shown
 * now, delivering the answer; returns a function that stops listening.
 */
export function answerSnapshots(subscribe: (fn: (ev: BusEvent) => void) => () => void, responder: SnapshotResponder, shown: () => Shown | undefined, deliver: (s: Snapshot) => Promise<unknown>): () => void {
  return subscribe((ev) => {
    const now = ev.type === 'snapshot' ? shown() : undefined;
    if (!now) return;
    responder
      .answer(ev.data as SnapshotRequest, now)
      .then((a) => a && deliver(a))
      .catch(() => {}); // another window answered first, or it came too late
  });
}
