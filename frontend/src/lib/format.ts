// Formatting helpers for values, units and times.
import type { Parameter } from './types';

/** Human-friendly relative time ("3m ago"), falling back to a date. */
export function relTime(iso: string | number | Date, now = Date.now()): string {
  const t = new Date(iso).getTime();
  if (!Number.isFinite(t)) return '';
  const s = Math.max(0, Math.round((now - t) / 1000));
  if (s < 5) return 'just now';
  if (s < 60) return `${s}s ago`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.round(h / 24);
  if (d < 30) return `${d}d ago`;
  return new Date(t).toLocaleDateString();
}

const prefixes: [number, string][] = [
  [1e9, 'G'], [1e6, 'M'], [1e3, 'k'], [1, ''], [1e-3, 'm'], [1e-6, 'µ'], [1e-9, 'n'], [1e-12, 'p'],
];

/** Three significant figures without trailing zeros. */
export function sig3(v: number): string {
  if (!Number.isFinite(v)) return String(v);
  if (v === 0) return '0';
  const s = Number(v.toPrecision(3));
  return String(s);
}

/**
 * Formats an SI value with a prefix, matching the Go calculators:
 * eng(4.7e-6, 'H') = "4.7 µH". Units that don't take prefixes print as is.
 */
export function eng(v: number | null | undefined, unit: string): string {
  if (v === null || v === undefined || !Number.isFinite(v)) return '–';
  if (unit === '%' || unit === '°C' || unit === '°C/W' || unit === '') return sig3(v) + (unit === '%' ? '%' : unit ? ` ${unit}` : '');
  if (v === 0) return `0 ${unit}`;
  const a = Math.abs(v);
  for (const [m, p] of prefixes) {
    if (a >= m * 0.9995) return `${sig3(v / m)} ${p}${unit}`;
  }
  return `${sig3(v / 1e-12)} p${unit}`;
}

/** A parameter's limits as "min / typ / max unit". */
export function limits(p: Pick<Parameter, 'min' | 'typ' | 'max' | 'unit'>): string {
  const f = (v?: number | null) => (v === null || v === undefined ? '–' : sig3(v));
  return `${f(p.min)} / ${f(p.typ)} / ${f(p.max)}${p.unit ? ' ' + p.unit : ''}`;
}

/** The headline value of a parameter: typ, else the range. */
export function headline(p: Pick<Parameter, 'min' | 'typ' | 'max' | 'unit'>): string {
  const u = p.unit ? ' ' + p.unit : '';
  const has = (v?: number | null) => v !== null && v !== undefined;
  if (has(p.min) && has(p.max)) return `${sig3(p.min!)} … ${sig3(p.max!)}${u}`;
  if (has(p.typ)) return `${sig3(p.typ!)}${u}`;
  if (has(p.max)) return `≤ ${sig3(p.max!)}${u}`;
  if (has(p.min)) return `≥ ${sig3(p.min!)}${u}`;
  return '–';
}

/** Section kind labels. */
export const sectionLabels: Record<string, string> = {
  abs_max: 'Absolute maximum',
  esd: 'ESD',
  recommended: 'Recommended operating',
  thermal: 'Thermal',
  electrical: 'Electrical',
  timing: 'Timing',
  typical_characteristics: 'Typical characteristics',
  other: 'Other',
};

/** Figure kind labels. */
export const figureKinds: Record<string, string> = {
  graph: 'Graph',
  schematic: 'Schematic',
  waveform: 'Waveform',
  block_diagram: 'Block diagram',
  pinout: 'Pinout',
  timing: 'Timing',
  layout: 'Layout',
  package: 'Package',
  figure: 'Figure',
};

/** Category labels. */
export const categories: Record<string, string> = {
  buck: 'Buck',
  boost: 'Boost',
  'buck-boost': 'Buck-boost',
  ldo: 'LDO / linear',
  charger: 'Charger',
  'mosfet-n': 'N-MOSFET',
  'mosfet-p': 'P-MOSFET',
  mosfet: 'MOSFET',
  'gate-driver': 'Gate driver',
  mcu: 'MCU / SoC',
  'current-monitor': 'Current monitor',
  'battery-protection': 'Battery protection',
  'load-switch': 'Load switch',
  efuse: 'eFuse',
  'fuel-gauge': 'Fuel gauge',
  tvs: 'TVS / ESD protection',
  amplifier: 'Amplifier',
  adc: 'ADC',
  dac: 'DAC',
  reference: 'Reference',
  diode: 'Diode',
  inductor: 'Inductor',
  capacitor: 'Capacitor',
  interface: 'Interface',
  sensor: 'Sensor',
  power: 'Power',
  resistor: 'Resistor',
  'capacitor-polarised': 'Polarised capacitor',
  ferrite: 'Ferrite bead',
  fuse: 'Fuse',
  crystal: 'Crystal',
  battery: 'Battery',
  schottky: 'Schottky diode',
  zener: 'Zener diode',
  led: 'LED',
  esd: 'ESD protection',
  npn: 'NPN transistor',
  pnp: 'PNP transistor',
  connector: 'Connector',
  switch: 'Switch',
  module: 'Module',
};

/** Parses a value typed with an SI prefix ("4.7u", "650k", "2.2µ") into a number. */
export function parseSI(s: string): number | undefined {
  const m = s.trim().replace(',', '.').match(/^([-+]?\d*\.?\d+(?:e[-+]?\d+)?)\s*([pnuµmkMG]?)/i);
  if (!m) return undefined;
  const mult: Record<string, number> = { p: 1e-12, n: 1e-9, u: 1e-6, µ: 1e-6, m: 1e-3, k: 1e3, K: 1e3, M: 1e6, G: 1e9, '': 1 };
  const p = m[2] === 'M' || m[2] === 'm' ? m[2] : m[2].toLowerCase() === 'k' ? 'k' : m[2];
  return parseFloat(m[1]) * (mult[p] ?? 1);
}

/** "1 part", "3 parts": a count with a word, pluralized with s. */
export function plural(n: number, word: string): string {
  return `${n.toLocaleString()} ${word}${n === 1 ? '' : 's'}`;
}
