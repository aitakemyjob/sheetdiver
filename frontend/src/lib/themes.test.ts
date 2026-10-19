import { applyTheme, cssVars, heatColor, hexA, heatScale, mix, themeById, themes } from './themes';

const hex = /^#[0-9a-f]{6}$/i;

describe('themes', () => {
  it('have unique ids and complete, valid palettes', () => {
    const ids = new Set(themes.map((t) => t.id));
    expect(ids.size).toBe(themes.length);
    for (const t of themes) {
      for (const key of ['bg', 'bg1', 'bg2', 'bg3', 'border', 'text', 'text2', 'accent', 'accent2', 'ok', 'warn', 'err'] as const) {
        expect(t[key], `${t.id}.${key}`).toMatch(hex);
      }
      expect(t.heat).toHaveLength(5);
      Object.values(t.syntax).forEach((c) => expect(c).toMatch(hex));
    }
  });

  it('falls back to the default theme', () => {
    expect(themeById('nope').id).toBe('deepdive');
    expect(themeById('nord').name).toBe('Nord');
  });

  it('includes light and dark themes', () => {
    expect(themes.some((t) => t.mode === 'light')).toBe(true);
    expect(themes.some((t) => t.mode === 'dark')).toBe(true);
  });

  it('applies css variables to the root', () => {
    const el = document.createElement('div');
    const t = themeById('dracula');
    applyTheme(t, el);
    expect(el.style.getPropertyValue('--accent')).toBe(t.accent);
    expect(el.dataset.mode).toBe('dark');
    expect(Object.keys(cssVars(t))).toContain('--heat-4');
  });
});

describe('heat', () => {
  const t = themeById('deepdive');
  it('maps zero to the neutral surface and one to the hottest stop', () => {
    expect(heatColor(t, 0)).toBe(t.bg3);
    expect(heatColor(t, 1)).toBe(t.heat[4]);
    expect(heatColor(t, 5)).toBe(t.heat[4]);
    expect(heatColor(t, NaN)).toBe(t.bg3);
  });
  it('interpolates between stops', () => {
    expect(heatColor(t, 0.125)).toBe(mix(t.heat[0], t.heat[1], 0.5));
  });
  it('uses a floored log scale', () => {
    expect(heatScale(100, 100)).toBe(1);
    expect(heatScale(1, 1)).toBeLessThan(0.5); // uniform tiny counts stay cool
    expect(heatScale(10, 100)).toBeGreaterThan(0.4);
    expect(heatScale(0, 100)).toBe(0);
    expect(heatScale(5, 0)).toBe(0);
  });
});

describe('hexA', () => {
  it('produces 8-digit hex that Monaco can parse', () => {
    expect(hexA('#22d3ee', 0.5)).toBe('#22d3ee80');
    expect(hexA('#000000', 0)).toBe('#00000000');
    expect(hexA('#ffffff', 2)).toBe('#ffffffff');
  });
});
