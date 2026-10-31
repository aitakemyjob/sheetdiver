import { api } from './api';
import type { CadDim, CadDrawing, CadLibrary, CadOutline, CadPackage, CadProblem, CadShape, CadState, CadView, PartCad } from './types';

/** How a symbol's or footprint's state reads: a badge's label, tone and explanation. */
export interface StateInfo {
  label: string;
  tone: '' | 'accent' | 'violet';
  title: string;
}

/** Each state's badge. */
const states: Record<CadState, StateInfo> = {
  auto: { label: 'SheetDiver', tone: '', title: 'Made by SheetDiver from the pin table or a standard package; redrawn when they change.' },
  drawn: { label: 'Agent', tone: 'accent', title: 'Drawn by an agent from the datasheet; redrawn from its drawing when the pin table changes.' },
  edited: { label: 'Your edit', tone: 'violet', title: 'Edited in your CAD tool: kept as you left it, never redrawn until you reset it.' },
};

/** The badge for a symbol's or footprint's state. */
export function stateInfo(state: CadState): StateInfo {
  return states[state] ?? states.auto;
}

/** What resetting a symbol or footprint does, for its button, or null when there's nothing to reset. */
export function resetAction(v: CadView | undefined): { label: string; confirm: string } | null {
  if (!v || v.state === 'auto') return null;
  if (v.state === 'edited') {
    return { label: 'Discard my edit', confirm: `Discard your edit of ${v.name}? It goes back to its last drawing (the agent's, or SheetDiver's own) and the library file is rewritten.` };
  }
  return { label: "Back to SheetDiver's", confirm: `Drop the agent's drawing of ${v.name} and go back to SheetDiver's own?` };
}

/** The SVG view box of a drawing. */
export function viewBox(d: CadDrawing): string {
  const v = d.view;
  return `${v.x0} ${v.y0} ${Math.max(v.x1 - v.x0, 0.01)} ${Math.max(v.y1 - v.y0, 0.01)}`;
}

/** A drawing's aspect ratio (height over width), to size it. */
export function aspect(d: CadDrawing): number {
  const v = d.view;
  return (v.y1 - v.y0) / Math.max(v.x1 - v.x0, 0.01);
}

/** The SVG attributes of a text shape: where it sits, how it's anchored and turned. */
export function textAttrs(s: CadShape): { x: number; y: number; fontSize: number; textAnchor: 'start' | 'middle' | 'end'; transform?: string } {
  const p = s.pts?.[0] ?? { x: 0, y: 0 };
  return {
    x: p.x,
    y: p.y,
    fontSize: s.size ?? 1,
    textAnchor: s.anchor ?? 'start',
    transform: s.vertical ? `rotate(-90 ${p.x} ${p.y})` : undefined,
  };
}

/** A polygon's points as SVG wants them. */
export function points(s: CadShape): string {
  return (s.pts ?? []).map((p) => `${p.x},${p.y}`).join(' ');
}

/** A problem, labelled with what it's about. */
export interface LabelledProblem extends CadProblem {
  about: string;
}

/** Every problem of a package: its symbol's, its footprint's, and between them. */
export function problemsOf(p: CadPackage): LabelledProblem[] {
  return [
    ...(p.symbol?.problems ?? []).map((x) => ({ ...x, about: 'Symbol' })),
    ...(p.footprint?.problems ?? []).map((x) => ({ ...x, about: 'Footprint' })),
    ...(p.match ?? []).map((x) => ({ ...x, about: 'Pins and pads' })),
  ];
}

/** A dimension as min/nom/max. */
export function dim(d: CadDim | undefined): string {
  if (!d) return '';
  const f = (v?: number) => (v ? String(+v.toFixed(3)) : '');
  if (d.min && d.max) return d.nom ? `${f(d.min)} / ${f(d.nom)} / ${f(d.max)}` : `${f(d.min)} – ${f(d.max)}`;
  return f(d.nom ?? d.min ?? d.max);
}

/** The package's dimensions worth showing, as rows of a table: what, the datasheet's symbol, value in mm. */
export function outlineRows(o: CadOutline | undefined): [string, string, string][] {
  if (!o) return [];
  const rows: [string, string, string][] = [
    ['Family', '', o.family + (o.name ? ` (${o.name})` : '')],
    ['Pins', '', String(o.pins)],
    ['Pitch', 'e', o.pitch ? String(o.pitch) : ''],
    ['Row spacing', '', dim(o.rowSpacing)],
    ['Pads drawn one by one', '', o.pads?.length ? String(o.pads.length) : ''],
    ['Body length', 'D', dim(o.bodyLength)],
    ['Body width', 'E1', dim(o.bodyWidth)],
    ['Span', 'E', dim(o.span)],
    ['Lead length', 'L', dim(o.leadLength)],
    ['Lead width', 'b', dim(o.leadWidth)],
    ['Height', 'A', dim(o.height)],
    ['Exposed pad', 'D2 × E2', o.exposedPad ? `${o.exposedPad.w} × ${o.exposedPad.h}` : ''],
  ];
  if (o.land?.drill) rows.push(['Land pattern', '', `pads ${o.land.padLength} across, ${o.land.drill} holes${o.land.page ? ` (p. ${o.land.page})` : ''}`]);
  else if (o.land) rows.push(['Land pattern', '', `pads ${o.land.padLength} × ${o.land.padWidth}, rows ${o.land.rowSpan} apart${o.land.page ? ` (p. ${o.land.page})` : ''}`]);
  return rows.filter((r) => r[2] !== '');
}

/** The formats a library is kept in, for people. */
export function formatsLabel(l: CadLibrary): string {
  const names = (l.formats ?? []).map((f) => l.available.find((a) => a.name === f)?.label ?? f);
  return names.length ? names.join(' and ') : 'no format (off)';
}

/** The formats the settings keep, as the backend reads them: none set is KiCad, ["none"] is none. */
export function settingsFormats(formats: string[] | undefined): string[] {
  if (!formats?.length) return ['kicad'];
  return formats.filter((f) => f !== 'none');
}

/** Whether a format is kept. */
export function keeps(l: CadLibrary, format: string): boolean {
  return (l.formats ?? []).includes(format);
}

/** The formats after turning one on or off; none left is saved as "none" (off). */
export function toggleFormat(l: CadLibrary, format: string, on: boolean): string[] {
  const now = new Set(l.formats ?? []);
  if (on) now.add(format);
  else now.delete(format);
  return now.size ? [...now].sort() : ['none'];
}

/** The CAD library's actions, for the views. */
export const cadActions = {
  /** Reset a symbol or footprint after the user confirms. */
  reset: (part: string, pkg: string, kind: 'symbol' | 'footprint', v: CadView | undefined, confirm: (msg: string) => boolean): Promise<PartCad> | null => {
    const a = resetAction(v);
    if (!a || !confirm(a.confirm)) return null;
    return api.resetCad(part, pkg, kind);
  },
  /** Sync the library, saying what it did. */
  sync: async (): Promise<string> => {
    const r = await api.syncCad();
    const kept = r.adopted?.length ? ` Kept your edits: ${r.adopted.join(', ')}.` : '';
    return `${r.symbols} symbols and ${r.footprints} footprints written.${kept}`;
  },
};
