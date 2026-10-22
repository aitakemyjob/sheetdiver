// Colours of the block diagram, by kind of part, for light and dark
// themes. They're plain colours rather than the theme's variables, so an
// exported SVG looks the same anywhere.

/** Fill, stroke and text colours of a kind of part. */
export type Swatch = { fill: string; stroke: string; text: string };

/** Colours by kind, for light and dark themes (as an electronics sketch: ICs violet, R and C green, diodes red, inductors amber). */
const palettes: Record<'light' | 'dark', Record<string, Swatch>> = {
  light: {
    ic: { fill: '#EEEDFE', stroke: '#534AB7', text: '#3C3489' },
    connector: { fill: '#F1EFE8', stroke: '#5F5E5A', text: '#444441' },
    module: { fill: '#F1EFE8', stroke: '#5F5E5A', text: '#444441' },
    resistor: { fill: '#E1F5EE', stroke: '#0F6E56', text: '#085041' },
    capacitor: { fill: '#E1F5EE', stroke: '#0F6E56', text: '#085041' },
    diode: { fill: '#FAECE7', stroke: '#993C1D', text: '#712B13' },
    inductor: { fill: '#FAEEDA', stroke: '#854F0B', text: '#633806' },
    other: { fill: '#F1EFE8', stroke: '#5F5E5A', text: '#444441' },
    wire: { fill: 'none', stroke: '#898781', text: '#52514E' },
  },
  dark: {
    ic: { fill: '#26215C', stroke: '#AFA9EC', text: '#CECBF6' },
    connector: { fill: '#2C2C2A', stroke: '#B4B2A9', text: '#D3D1C7' },
    module: { fill: '#2C2C2A', stroke: '#B4B2A9', text: '#D3D1C7' },
    resistor: { fill: '#04342C', stroke: '#5DCAA5', text: '#9FE1CB' },
    capacitor: { fill: '#04342C', stroke: '#5DCAA5', text: '#9FE1CB' },
    diode: { fill: '#4A1B0C', stroke: '#F0997B', text: '#F5C4B3' },
    inductor: { fill: '#412402', stroke: '#EF9F27', text: '#FAC775' },
    other: { fill: '#2C2C2A', stroke: '#B4B2A9', text: '#D3D1C7' },
    wire: { fill: 'none', stroke: '#8B8A85', text: '#B4B2A9' },
  },
};

/** Whether the theme is dark, from its background colour. */
export function darkTheme(): boolean {
  const bg = getComputedStyle(document.documentElement).getPropertyValue('--bg').trim();
  const m = /^#?([\da-f]{2})([\da-f]{2})([\da-f]{2})/i.exec(bg);
  if (!m) return false;
  const [r, g, b] = m.slice(1).map((h) => parseInt(h, 16));
  return 0.299 * r + 0.587 * g + 0.114 * b < 128;
}

/** The palette for the current theme. */
export function palette(): Record<string, Swatch> {
  return palettes[darkTheme() ? 'dark' : 'light'];
}

/** A kind's colours (unknown kinds look like a connector). */
export function swatch(pal: Record<string, Swatch>, kind: string): Swatch {
  return pal[kind] ?? pal.connector;
}
