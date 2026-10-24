import { describe, expect, it, vi } from 'vitest';
import type { BlockDrawing } from '../blockDiagram';
import type { BusEvent, DiagramGroup } from '../types';
import { answerSnapshots, cropOf, MAX_EDGE, pictureSize, SnapshotResponder, standaloneSVG, type Rasterize, type Shown } from './snapshot';

const frame = (name: string, x: number, y: number, w: number, h: number) => ({ x, y, w, h, header: 40, group: { name } as DiagramGroup });
const drawing = (frames = [frame('Power', 0, 0, 300, 200), frame('MCU', 360, 0, 500, 400)]): BlockDrawing => ({
  boxes: [], passives: [], wires: [], grounds: [], flags: [], dots: [], clusters: [], cells: [], frames, width: 900, height: 420,
});

/** The app's SVG: zoomed and panned, coloured with the theme's variables. */
function shownSVG(): SVGSVGElement {
  const el = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  el.setAttribute('style', 'height: 380px; font-family: var(--font-ui)');
  el.innerHTML = '<g data-view="" transform="translate(40,20) scale(0.5)"><rect x="10" y="10" width="20" height="20" fill="var(--accent)"/></g>';
  return el;
}
const resolve = (name: string) => ({ '--accent': '#c00', '--bg': '#fafafa', '--font-ui': '"Inter", system-ui' })[name] ?? '';

describe('pictures of the block diagram', () => {
  it('stands the SVG alone: full size, cropped, theme resolved, over a background', () => {
    const svg = standaloneSVG(shownSVG(), { x: 350, y: -10, w: 520, h: 420 }, 1040, 840, resolve, '#fafafa');
    expect(svg).toContain('xmlns="http://www.w3.org/2000/svg"');
    expect(svg).toContain('viewBox="350 -10 520 420"');
    expect(svg).toContain('width="1040"');
    expect(svg).not.toContain('transform=');
    expect(svg).not.toContain('style=');
    expect(svg).toContain('fill="#c00"');
    expect(svg).toContain(`font-family="'Inter', system-ui, sans-serif"`);
    // It still parses as XML.
    expect(new DOMParser().parseFromString(svg, 'image/svg+xml').querySelector('parsererror')).toBeNull();
    expect(svg).toMatch(/<svg[^>]*><rect x="350" y="-10" width="520" height="420" fill="#fafafa"/);
  });

  it("looks at the whole drawing, or one group's frame with a margin, or says what groups there are", () => {
    expect(cropOf(drawing())).toEqual({ x: 0, y: 0, w: 900, h: 420 });
    expect(cropOf(drawing(), 'mcu')).toEqual({ x: 350, y: -10, w: 520, h: 420 });
    expect(cropOf(drawing(), 'Clock')).toBe('no group "Clock" on the diagram; its groups are Power, MCU');
    expect(cropOf(drawing([]), 'Clock')).toBe('the diagram has no groups, so there\'s no "Clock"');
  });

  it('fits a big drawing in the largest picture the model takes, and sharpens a small one', () => {
    expect(pictureSize({ x: 0, y: 0, w: 3136, h: 1000 })).toEqual({ width: MAX_EDGE, height: 500 });
    expect(pictureSize({ x: 0, y: 0, w: 300, h: 200 })).toEqual({ width: 600, height: 400 });
  });

  it("answers a request for its project with the picture, and leaves another project's", async () => {
    const rasterize = vi.fn<Rasterize>(async () => 'iVBORw0KGgo=');
    const r = new SnapshotResponder(rasterize, resolve);
    const shown: Shown = { project: 'Board', drawing: drawing(), svg: shownSVG() };
    expect(await r.answer({ id: '1', project: 'board', group: 'Power' }, shown)).toEqual({ id: '1', png: 'iVBORw0KGgo=', width: 640, height: 440 });
    expect(rasterize).toHaveBeenCalledWith(expect.stringContaining('viewBox="-10 -10 320 220"'), 640, 440);
    expect(await r.answer({ id: '2', project: 'other' }, shown)).toBeUndefined();
    expect(await r.answer({ id: '3', project: 'board', group: 'Clock' }, shown)).toEqual({ id: '3', width: 0, height: 0, problem: 'no group "Clock" on the diagram; its groups are Power, MCU' });
    const broken = new SnapshotResponder(async () => Promise.reject(new Error('no canvas')), resolve);
    expect(await broken.answer({ id: '4', project: 'board' }, shown)).toMatchObject({ id: '4', problem: 'no canvas' });
  });

  it('answers picture requests from the events while something is shown, and stops when told', async () => {
    let listener: ((ev: BusEvent) => void) | undefined;
    const subscribe = (fn: (ev: BusEvent) => void) => ((listener = fn), () => (listener = undefined));
    const deliver = vi.fn(async () => {});
    const screen: { shown?: Shown } = {};
    const stop = answerSnapshots(subscribe, new SnapshotResponder(async () => 'png', resolve), () => screen.shown, deliver);
    listener!({ type: 'snapshot', time: '', data: { id: '1', project: 'board' } });
    screen.shown = { project: 'board', drawing: drawing(), svg: shownSVG() };
    listener!({ type: 'focus', time: '', data: { id: '2', project: 'board' } });
    listener!({ type: 'snapshot', time: '', data: { id: '3', project: 'board' } });
    await vi.waitFor(() => expect(deliver).toHaveBeenCalledTimes(1));
    expect(deliver).toHaveBeenCalledWith(expect.objectContaining({ id: '3', png: 'png' }));
    stop();
    expect(listener).toBeUndefined();
  });
});
