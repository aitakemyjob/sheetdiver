// What the Simulations tab draws, worked out from the backend's data:
// which runs each of the agent's charts needs (the variations it
// compares, in its conditions), the chart's scales, ticks, colours and
// labels, the value under the cursor, and the targets as a matrix of
// variations against conditions. Nothing here draws.

import { eng } from './format';
import type { RunState, SimPlot, SimRun, SimulationData, SimulationDetail, VariationDetail } from './types';

/** A chart panel: one of the agent's charts, or its magnitude or phase half for AC. */
export interface Panel {
  key: string;
  title: string;
  spec: SimPlot;
  /** For AC: mag or phase; '' otherwise. */
  part: '' | 'mag' | 'phase';
}

/** Whether a plot id is an AC analysis's (complex values). */
export function isAC(plot: string): boolean {
  return /^ac\d+$/.test(plot);
}

/** The panels of the agent's charts: an AC chart with no part asked for shows its magnitude and phase as two charts, each on one axis. */
export function panels(plots: SimPlot[]): Panel[] {
  const out: Panel[] = [];
  plots.forEach((p, i) => {
    if (!isAC(p.plot)) {
      out.push({ key: `${i}`, title: p.title, spec: p, part: '' });
      return;
    }
    const parts: ('mag' | 'phase')[] = p.part === 'phase' ? ['phase'] : p.part === 'mag' ? ['mag'] : ['mag', 'phase'];
    for (const part of parts) out.push({ key: `${i}-${part}`, title: parts.length > 1 ? `${p.title} — ${part === 'mag' ? 'magnitude' : 'phase'}` : p.title, spec: p, part });
  });
  return out;
}

/** Whether a run has results to draw. */
export function hasResults(r: SimRun): boolean {
  return r.state === 'done' || (r.state === 'stale' && r.plots.length > 0);
}

/** The variations a chart compares at first: those the agent named, else the latest. */
export function defaultVariations(spec: SimPlot, variations: VariationDetail[]): string[] {
  const names = variations.map((v) => v.variation.name);
  const asked = (spec.variations ?? []).filter((n) => names.some((x) => x.toLowerCase() === n.toLowerCase()));
  return asked.length ? asked : names.slice(-1);
}

/** The runs a chart draws: the latest run of each of its conditions (all when it names none) in each chosen variation, with results and the plot. */
export function runsFor(spec: SimPlot, variations: VariationDetail[], chosen: string[]): SimRun[] {
  const want = (spec.conditions ?? []).map((c) => c.toLowerCase());
  const pick = chosen.map((c) => c.toLowerCase());
  return variations
    .filter((v) => pick.includes(v.variation.name.toLowerCase()))
    .flatMap((v) => v.latest)
    .filter((r) => hasResults(r) && r.plots.some((p) => p.id === spec.plot) && (want.length === 0 || want.includes(r.condition.toLowerCase())));
}

/** Categorical colours in fixed order, stepped for light and dark surfaces (validated for colour-vision deficiency). */
const SERIES_LIGHT = ['#2a78d6', '#eb6834', '#1baf7a', '#eda100', '#e87ba4', '#008300', '#6250d6', '#e34948'];
const SERIES_DARK = ['#3987e5', '#d95926', '#199e70', '#c98500', '#d55181', '#008300', '#9085e9', '#e66767'];

/** The colour of the i-th series; past eight, they repeat with a dash so identity is never colour alone. */
export function seriesStyle(i: number, dark: boolean): { color: string; dash: string } {
  const set = dark ? SERIES_DARK : SERIES_LIGHT;
  return { color: set[i % set.length], dash: i >= set.length ? '6 4' : '' };
}

/** A line of a chart: which signal of which variant, its style and points. */
export interface ChartSeries {
  key: string;
  label: string;
  /** What its line's end is labelled: the variant alone when every line is the same signal. */
  short: string;
  color: string;
  dash: string;
  x: number[];
  y: number[];
}

/** A tick: where, and its label. */
export interface Tick {
  v: number;
  label: string;
}

/** A chart ready to draw. */
export interface ChartModel {
  series: ChartSeries[];
  xLog: boolean;
  x: [number, number];
  y: [number, number];
  xTicks: Tick[];
  yTicks: Tick[];
  xLabel: string;
  yLabel: string;
  xUnit: string;
  yUnit: string;
}

/** A unit as eng() writes values in it: dB and degrees take no prefix. */
function fmt(v: number, unit: string): string {
  if (unit === 'dB' || unit === '°') return `${Number(v.toPrecision(3))} ${unit}`.replace(' °', '°');
  return eng(v, unit);
}

/** Round numbers between min and max, about count of them. */
export function niceTicks(min: number, max: number, count = 5): number[] {
  if (!isFinite(min) || !isFinite(max)) return [];
  if (min === max) return [min];
  const raw = (max - min) / Math.max(count, 1);
  const mag = 10 ** Math.floor(Math.log10(raw));
  const step = [1, 2, 2.5, 5, 10].map((m) => m * mag).find((s) => s >= raw) ?? 10 * mag;
  const out: number[] = [];
  for (let v = Math.ceil(min / step) * step; v <= max + step * 1e-9; v += step) out.push(Math.abs(v) < step * 1e-9 ? 0 : v);
  return out;
}

/** Decades between min and max (both > 0), for a log axis. */
export function logTicks(min: number, max: number): number[] {
  if (!(min > 0) || !(max > 0)) return [];
  const out: number[] = [];
  for (let e = Math.ceil(Math.log10(min) - 1e-9); e <= Math.floor(Math.log10(max) + 1e-9); e++) out.push(10 ** e);
  return out;
}

/** A scale from data to pixels, linear or logarithmic. */
export function scale(domain: [number, number], range: [number, number], log = false): (v: number) => number {
  const f = log ? (v: number) => Math.log10(Math.max(v, 1e-300)) : (v: number) => v;
  const [d0, d1] = [f(domain[0]), f(domain[1])];
  const span = d1 - d0 || 1;
  return (v) => range[0] + ((f(v) - d0) / span) * (range[1] - range[0]);
}

/** The chart of a panel from its runs' data: a line per signal, variation and condition, a shared x axis (log for frequency, or when asked), one y axis. */
export function chartModel(panel: Panel, data: SimulationData[], dark: boolean): ChartModel {
  const series: ChartSeries[] = [];
  // Each line is named by what tells it apart: its signal, variation and
  // condition, leaving out what all the lines share.
  const signals = new Set(data.flatMap((d) => d.series.map((s) => s.name)));
  const byVariation = new Set(data.map((d) => d.variation)).size > 1;
  const byCondition = new Set(data.map((d) => d.condition)).size > 1;
  for (const d of data) {
    for (const s of d.series) {
      const style = seriesStyle(series.length, dark);
      const which = [byVariation ? d.variation : '', byCondition ? d.condition : ''].filter(Boolean);
      const label = [s.name, ...which].join(' · ');
      series.push({ key: `${d.run}:${s.name}`, label, short: signals.size === 1 && which.length ? which.join(' · ') : label, ...style, x: s.x, y: s.y });
    }
  }
  const xUnit = data[0]?.xUnit ?? '';
  const yUnit = data[0]?.series[0]?.unit ?? '';
  const xLog = !!panel.spec.xLog || (isAC(panel.spec.plot) && xUnit === 'Hz');
  let [x0, x1, y0, y1] = [Infinity, -Infinity, Infinity, -Infinity];
  for (const s of series) {
    for (let i = 0; i < s.x.length; i++) {
      if (xLog && !(s.x[i] > 0)) continue;
      x0 = Math.min(x0, s.x[i]);
      x1 = Math.max(x1, s.x[i]);
      if (isFinite(s.y[i])) {
        y0 = Math.min(y0, s.y[i]);
        y1 = Math.max(y1, s.y[i]);
      }
    }
  }
  if (!isFinite(x0)) [x0, x1] = [0, 1];
  if (!isFinite(y0)) [y0, y1] = [0, 1];
  // Room above and below the traces, and a flat trace given height.
  const pad = (y1 - y0) * 0.06 || Math.abs(y0) * 0.1 || 1;
  const y: [number, number] = [y0 - pad, y1 + pad];
  const xs = xLog ? logTicks(x0, x1) : niceTicks(x0, x1, 6);
  const ys = niceTicks(y[0], y[1], 5);
  return {
    series,
    xLog,
    x: [x0, x1],
    y,
    xTicks: xs.map((v) => ({ v, label: fmt(v, xUnit) })),
    yTicks: ys.map((v) => ({ v, label: fmt(v, yUnit) })),
    xLabel: data[0]?.xName ?? '',
    yLabel: panel.spec.yLabel || yUnit,
    xUnit,
    yUnit,
  };
}

/** The x axis's name: a DC sweep's scale (ngspice calls it v-sweep or i-sweep) named after the source the analysis sweeps. */
export function axisName(xName: string, plot: string, analyses: { kind: string; line: string }[]): string {
  if (!/sweep/i.test(xName)) return xName;
  const m = /^dc(\d+)$/.exec(plot);
  const dcs = analyses.filter((a) => a.kind === 'dc');
  const line = m ? dcs[Number(m[1]) - 1]?.line : undefined;
  const source = line?.split(/\s+/)[1];
  return source ? `${source} (swept)` : xName;
}

/** Labels at the lines' ends, spread at least gap apart within [top, bottom]; none when they can't all fit (the legend names the lines). */
export function endLabels(ys: number[], top: number, bottom: number, gap = 12): (number | null)[] {
  if (ys.length === 0) return [];
  if ((ys.length - 1) * gap > bottom - top) return ys.map(() => null);
  const order = ys.map((y, i) => ({ y: Math.min(Math.max(y, top), bottom), i })).sort((a, b) => a.y - b.y);
  for (let k = 1; k < order.length; k++) order[k].y = Math.max(order[k].y, order[k - 1].y + gap);
  // Pushed past the bottom: shift the stack back up.
  const over = order[order.length - 1].y - bottom;
  if (over > 0) for (const o of order) o.y -= over;
  const out: (number | null)[] = ys.map(() => null);
  for (const o of order) out[o.i] = o.y;
  return out;
}

/** The index of the point nearest x in a sorted list. */
export function nearestIndex(xs: number[], x: number): number {
  if (xs.length === 0) return -1;
  let lo = 0;
  let hi = xs.length - 1;
  while (hi - lo > 1) {
    const mid = (lo + hi) >> 1;
    if (xs[mid] < x) lo = mid;
    else hi = mid;
  }
  return Math.abs(xs[lo] - x) <= Math.abs(xs[hi] - x) ? lo : hi;
}

/** What the cursor reads at x: each series's value at its nearest point. */
export function readout(m: ChartModel, x: number): { x: string; rows: { label: string; color: string; value: string }[] } {
  const rows = m.series.map((s) => {
    const i = nearestIndex(s.x, x);
    return { label: s.label, color: s.color, value: i < 0 ? '–' : fmt(s.y[i], m.yUnit) };
  });
  return { x: fmt(x, m.xUnit), rows };
}

/** A value with its unit, for tables. */
export function formatValue(v: number | undefined, unit = ''): string {
  return v === undefined || v === null ? '–' : fmt(v, unit);
}

/** A target as a matrix: each variation's value and verdict in each condition. */
export interface TargetMatrix {
  measure: string;
  unit: string;
  range: string;
  why?: string;
  rows: { variation: string; change?: string; cells: { condition: string; value: string; pass?: boolean }[] }[];
}

/** The conditions the variations ran in, in the order they first appear. */
export function conditionsOf(d: SimulationDetail): string[] {
  const out: string[] = [];
  for (const v of d.variations) for (const r of v.latest) if (!out.includes(r.condition)) out.push(r.condition);
  return out;
}

/** The targets, one matrix per measurement: a row per variation, in the order they were added, and a cell per condition. */
export function targetMatrix(d: SimulationDetail): TargetMatrix[] {
  const checks = d.simulation.presentation?.checks ?? [];
  const conds = conditionsOf(d);
  return checks.map((c) => {
    const unit = c.unit ?? '';
    const range = c.min !== undefined && c.max !== undefined ? `${fmt(c.min, unit)} … ${fmt(c.max, unit)}` : c.min !== undefined ? `≥ ${fmt(c.min, unit)}` : c.max !== undefined ? `≤ ${fmt(c.max, unit)}` : 'any';
    return {
      measure: c.measure,
      unit,
      range,
      why: c.why,
      rows: d.variations.map((v) => ({
        variation: v.variation.name,
        change: v.variation.change,
        cells: conds.map((cond) => {
          const r = v.checks.find((x) => x.measure === c.measure && x.condition === cond);
          return { condition: cond, value: r ? formatValue(r.value, unit) : '', pass: r?.pass };
        }),
      })),
    };
  });
}

/** The agent's note on a variation, if it wrote one. */
export function noteOf(d: SimulationDetail, variation: string): string | undefined {
  return d.simulation.presentation?.notes?.find((n) => n.variation.toLowerCase() === variation.toLowerCase())?.text;
}

/** How a run's state is shown: its badge's tone and words. */
export function stateBadge(state: RunState): { tone: '' | 'ok' | 'warn' | 'err'; label: string } {
  switch (state) {
    case 'done':
      return { tone: 'ok', label: 'done' };
    case 'failed':
      return { tone: 'err', label: 'failed' };
    case 'stale':
      return { tone: 'warn', label: 'stale: changed since' };
    case 'running':
    case 'queued':
      return { tone: '', label: state };
    default:
      return { tone: 'warn', label: state };
  }
}
