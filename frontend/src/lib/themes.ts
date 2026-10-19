// Themes are plain palettes shared with CodeDiver. One palette drives the
// CSS variables and the chart colors, so every surface of the app stays
// coherent when switching.

export interface Syntax {
  keyword: string;
  string: string;
  number: string;
  comment: string;
  type: string;
  func: string;
  variable: string;
  operator: string;
}

export interface Theme {
  id: string;
  name: string;
  mode: 'dark' | 'light';
  bg: string;
  bg1: string;
  bg2: string;
  bg3: string;
  border: string;
  borderStrong: string;
  text: string;
  text2: string;
  text3: string;
  accent: string;
  accent2: string;
  accentFg: string;
  ok: string;
  warn: string;
  err: string;
  info: string;
  /** Five stops from cold to hot. */
  heat: [string, string, string, string, string];
  syntax: Syntax;
}

export const themes: Theme[] = [
  {
    // Default: the Diver palette — deep-sea navy, sky blue, signal yellow.
    id: 'deepdive', name: 'Deep Dive', mode: 'dark',
    // Near-neutral darks with a faint navy tint; the logo's sky blue and
    // code yellow are reserved for accents so they stay meaningful.
    bg: '#0b1120', bg1: '#0e1526', bg2: '#131b2e', bg3: '#1a2338', border: '#1f2940', borderStrong: '#2b3752',
    text: '#e4ecf5', text2: '#9aa9be', text3: '#5f6d84', accent: '#4fc3f7', accent2: '#f5d76e', accentFg: '#06121f',
    ok: '#4ade80', warn: '#f5d76e', err: '#f87171', info: '#7dd3fc',
    heat: ['#1d3a5c', '#2f7fbf', '#4fc3f7', '#f5d76e', '#f87171'],
    syntax: { keyword: '#4fc3f7', string: '#f5d76e', number: '#f0a868', comment: '#56657d', type: '#7dd3fc', func: '#a8c8ff', variable: '#e4ecf5', operator: '#9aa9be' },
  },
  {
    id: 'abyss', name: 'Abyss', mode: 'dark',
    bg: '#0b0e14', bg1: '#0f131b', bg2: '#151a24', bg3: '#1c2230', border: '#212836', borderStrong: '#2d3648',
    text: '#e6edf3', text2: '#9aa7b8', text3: '#5f6b7e', accent: '#22d3ee', accent2: '#a78bfa', accentFg: '#041016',
    ok: '#34d399', warn: '#fbbf24', err: '#f87171', info: '#60a5fa',
    heat: ['#16324f', '#1d4ed8', '#9333ea', '#f97316', '#ef4444'],
    syntax: { keyword: '#c792ea', string: '#9ece6a', number: '#ff9e64', comment: '#56637a', type: '#2ac3de', func: '#82aaff', variable: '#e6edf3', operator: '#89ddff' },
  },
  {
    id: 'obsidian', name: 'Obsidian', mode: 'dark',
    bg: '#0c0c0e', bg1: '#121215', bg2: '#19191d', bg3: '#212126', border: '#26262c', borderStrong: '#34343c',
    text: '#ececf1', text2: '#a1a1aa', text3: '#66666f', accent: '#f59e0b', accent2: '#f43f5e', accentFg: '#1a1002',
    ok: '#4ade80', warn: '#facc15', err: '#fb7185', info: '#38bdf8',
    heat: ['#2a2a30', '#57534e', '#b45309', '#f59e0b', '#f43f5e'],
    syntax: { keyword: '#f59e0b', string: '#a3e635', number: '#fb923c', comment: '#5c5c66', type: '#38bdf8', func: '#fde68a', variable: '#ececf1', operator: '#f9a8d4' },
  },
  {
    id: 'tokyo', name: 'Tokyo Night', mode: 'dark',
    bg: '#14141c', bg1: '#1a1b26', bg2: '#1f2233', bg3: '#292e42', border: '#272b3f', borderStrong: '#3b4261',
    text: '#c0caf5', text2: '#9aa5ce', text3: '#565f89', accent: '#7aa2f7', accent2: '#bb9af7', accentFg: '#0b1020',
    ok: '#9ece6a', warn: '#e0af68', err: '#f7768e', info: '#7dcfff',
    heat: ['#1f2a4a', '#3d59a1', '#9d7cd8', '#ff9e64', '#f7768e'],
    syntax: { keyword: '#bb9af7', string: '#9ece6a', number: '#ff9e64', comment: '#565f89', type: '#2ac3de', func: '#7aa2f7', variable: '#c0caf5', operator: '#89ddff' },
  },
  {
    id: 'nord', name: 'Nord', mode: 'dark',
    bg: '#272c36', bg1: '#2e3440', bg2: '#353c4a', bg3: '#3b4252', border: '#3e4656', borderStrong: '#4c566a',
    text: '#eceff4', text2: '#c2cad8', text3: '#7b879c', accent: '#88c0d0', accent2: '#b48ead', accentFg: '#1c2129',
    ok: '#a3be8c', warn: '#ebcb8b', err: '#bf616a', info: '#81a1c1',
    heat: ['#3b4252', '#5e81ac', '#b48ead', '#d08770', '#bf616a'],
    syntax: { keyword: '#81a1c1', string: '#a3be8c', number: '#b48ead', comment: '#616e88', type: '#8fbcbb', func: '#88c0d0', variable: '#d8dee9', operator: '#81a1c1' },
  },
  {
    id: 'dracula', name: 'Dracula', mode: 'dark',
    bg: '#1b1c25', bg1: '#21222c', bg2: '#282a36', bg3: '#343746', border: '#30323f', borderStrong: '#44475a',
    text: '#f8f8f2', text2: '#c3c4d4', text3: '#6272a4', accent: '#bd93f9', accent2: '#ff79c6', accentFg: '#17121f',
    ok: '#50fa7b', warn: '#f1fa8c', err: '#ff5555', info: '#8be9fd',
    heat: ['#2f3242', '#6272a4', '#bd93f9', '#ffb86c', '#ff5555'],
    syntax: { keyword: '#ff79c6', string: '#f1fa8c', number: '#bd93f9', comment: '#6272a4', type: '#8be9fd', func: '#50fa7b', variable: '#f8f8f2', operator: '#ff79c6' },
  },
  {
    id: 'gruvbox', name: 'Gruvbox', mode: 'dark',
    bg: '#1a1c1d', bg1: '#202222', bg2: '#282828', bg3: '#32302f', border: '#3c3836', borderStrong: '#504945',
    text: '#ebdbb2', text2: '#bdae93', text3: '#7c6f64', accent: '#fabd2f', accent2: '#8ec07c', accentFg: '#1d1a0e',
    ok: '#b8bb26', warn: '#fabd2f', err: '#fb4934', info: '#83a598',
    heat: ['#32302f', '#458588', '#b16286', '#fe8019', '#fb4934'],
    syntax: { keyword: '#fb4934', string: '#b8bb26', number: '#d3869b', comment: '#7c6f64', type: '#fabd2f', func: '#8ec07c', variable: '#ebdbb2', operator: '#fe8019' },
  },
  {
    id: 'synthwave', name: 'Synthwave', mode: 'dark',
    bg: '#120d22', bg1: '#17112b', bg2: '#1f1739', bg3: '#2a1f4b', border: '#2e2352', borderStrong: '#45367a',
    text: '#f5e9ff', text2: '#c7b6ea', text3: '#7d6ca8', accent: '#ff3cac', accent2: '#2de2e6', accentFg: '#1b0414',
    ok: '#72f1b8', warn: '#fede5d', err: '#fe4450', info: '#36f9f6',
    heat: ['#2a1f4b', '#3b5bdb', '#b23adf', '#ff7edb', '#fe4450'],
    syntax: { keyword: '#fede5d', string: '#ff8b39', number: '#f97e72', comment: '#7d6ca8', type: '#fe4450', func: '#36f9f6', variable: '#ff7edb', operator: '#fede5d' },
  },
  {
    id: 'paper', name: 'Paper', mode: 'light',
    bg: '#f3f4f7', bg1: '#ffffff', bg2: '#f6f7f9', bg3: '#eceef2', border: '#e2e5ea', borderStrong: '#cfd4dc',
    text: '#1a2130', text2: '#525d70', text3: '#8b94a5', accent: '#2563eb', accent2: '#7c3aed', accentFg: '#ffffff',
    ok: '#059669', warn: '#d97706', err: '#dc2626', info: '#0284c7',
    heat: ['#e0e7ff', '#93c5fd', '#a78bfa', '#fb923c', '#ef4444'],
    syntax: { keyword: '#7c3aed', string: '#0a7a3b', number: '#c2410c', comment: '#9aa3b2', type: '#0e7490', func: '#1d4ed8', variable: '#1a2130', operator: '#be185d' },
  },
  {
    id: 'solarized', name: 'Solarized Light', mode: 'light',
    bg: '#eee8d5', bg1: '#fdf6e3', bg2: '#f7f0dc', bg3: '#ece5cf', border: '#e0d9c2', borderStrong: '#cfc7ad',
    text: '#073642', text2: '#586e75', text3: '#93a1a1', accent: '#268bd2', accent2: '#d33682', accentFg: '#fdf6e3',
    ok: '#859900', warn: '#b58900', err: '#dc322f', info: '#2aa198',
    heat: ['#eee8d5', '#93c5d8', '#6c71c4', '#cb4b16', '#dc322f'],
    syntax: { keyword: '#859900', string: '#2aa198', number: '#d33682', comment: '#93a1a1', type: '#b58900', func: '#268bd2', variable: '#073642', operator: '#cb4b16' },
  },
];

/** Looks up a theme, falling back to the default (Deep Dive). */
export function themeById(id: string | undefined): Theme {
  return themes.find((t) => t.id === id) ?? themes[0];
}

/** Parses #rgb or #rrggbb. */
function hexToRgb(hex: string): [number, number, number] {
  const h = hex.replace('#', '');
  const n = parseInt(h.length === 3 ? h.split('').map((c) => c + c).join('') : h, 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

/** Formats an RGB triple as #rrggbb, clamping channels. */
function rgbToHex([r, g, b]: [number, number, number]): string {
  return '#' + [r, g, b].map((v) => Math.round(Math.max(0, Math.min(255, v))).toString(16).padStart(2, '0')).join('');
}

/** Linearly interpolates between two hex colors (t in 0..1). */
export function mix(a: string, b: string, t: number): string {
  const x = hexToRgb(a);
  const y = hexToRgb(b);
  return rgbToHex([x[0] + (y[0] - x[0]) * t, x[1] + (y[1] - x[1]) * t, x[2] + (y[2] - x[2]) * t]);
}

/** A hex color with alpha, as a CSS rgba() string. */
export function alpha(hex: string, a: number): string {
  const [r, g, b] = hexToRgb(hex);
  return `rgba(${r}, ${g}, ${b}, ${a})`;
}

/** #rrggbbaa form. */
export function hexA(hex: string, a: number): string {
  return hex.slice(0, 7) + Math.round(Math.max(0, Math.min(1, a)) * 255).toString(16).padStart(2, '0');
}

/** Maps t∈[0,1] onto the theme's five heat stops. */
export function heatColor(theme: Theme, t: number): string {
  if (!Number.isFinite(t) || t <= 0) return theme.bg3;
  const c = Math.min(1, t);
  const seg = c * (theme.heat.length - 1);
  const i = Math.min(theme.heat.length - 2, Math.floor(seg));
  return mix(theme.heat[i], theme.heat[i + 1], seg - i);
}

/**
 * Normalizes a value against the max on a log scale. The denominator is
 * floored so a repo where every file has 1–2 commits doesn't render as a
 * wall of "hot" cells.
 */
export function heatScale(value: number, maxValue: number, floor = 4): number {
  if (!maxValue || value <= 0) return 0;
  return Math.min(1, Math.log1p(value) / Math.log1p(Math.max(maxValue, floor)));
}

/** The CSS custom properties a theme defines. */
export function cssVars(t: Theme): Record<string, string> {
  return {
    '--bg': t.bg, '--bg-1': t.bg1, '--bg-2': t.bg2, '--bg-3': t.bg3,
    '--border': t.border, '--border-strong': t.borderStrong,
    '--text': t.text, '--text-2': t.text2, '--text-3': t.text3,
    '--accent': t.accent, '--accent-2': t.accent2, '--accent-fg': t.accentFg,
    '--accent-soft': alpha(t.accent, t.mode === 'dark' ? 0.14 : 0.1),
    '--accent-2-soft': alpha(t.accent2, t.mode === 'dark' ? 0.16 : 0.1),
    '--ok': t.ok, '--warn': t.warn, '--err': t.err, '--info': t.info,
    '--ok-soft': alpha(t.ok, 0.14), '--warn-soft': alpha(t.warn, 0.16), '--err-soft': alpha(t.err, 0.14),
    '--shadow': t.mode === 'dark' ? '0 12px 40px rgba(0,0,0,.45)' : '0 12px 32px rgba(20,30,50,.12)',
    '--grid': alpha(t.text3, t.mode === 'dark' ? 0.18 : 0.25),
    '--heat-0': t.heat[0], '--heat-1': t.heat[1], '--heat-2': t.heat[2], '--heat-3': t.heat[3], '--heat-4': t.heat[4],
    '--syn-keyword': t.syntax.keyword, '--syn-string': t.syntax.string, '--syn-type': t.syntax.type, '--syn-func': t.syntax.func,
  };
}

/**
 * Applies a theme to the document (CSS variables, data attributes and
 * color-scheme).
 */
export function applyTheme(t: Theme, root: HTMLElement = document.documentElement) {
  for (const [k, v] of Object.entries(cssVars(t))) root.style.setProperty(k, v);
  root.dataset.theme = t.id;
  root.dataset.mode = t.mode;
  root.style.colorScheme = t.mode;
}
