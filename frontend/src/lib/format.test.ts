import { eng, headline, limits, parseSI, relTime } from './format';
import { snippetHTML } from '../views/SearchView';

describe('format', () => {
  it('formats SI values like the Go calculators', () => {
    expect(eng(4.7e-6, 'H')).toBe('4.7 µH');
    expect(eng(650e3, 'Hz')).toBe('650 kHz');
    expect(eng(0.0025, 'A')).toBe('2.5 mA');
    expect(eng(3.3, 'V')).toBe('3.3 V');
    expect(eng(87.9, '°C/W')).toBe('87.9 °C/W');
    expect(eng(undefined, 'V')).toBe('–');
  });

  it('parses values typed with prefixes', () => {
    expect(parseSI('4.7u')).toBeCloseTo(4.7e-6);
    expect(parseSI('4.7µ')).toBeCloseTo(4.7e-6);
    expect(parseSI('650k')).toBe(650000);
    expect(parseSI('2m')).toBeCloseTo(0.002);
    expect(parseSI('2M')).toBe(2e6);
    expect(parseSI('12')).toBe(12);
    expect(parseSI('abc')).toBeUndefined();
  });

  it('shows parameter limits and headlines', () => {
    expect(limits({ min: 4.5, typ: null, max: 17, unit: 'V' })).toBe('4.5 / – / 17 V');
    expect(headline({ min: 4.5, max: 17, unit: 'V' })).toBe('4.5 … 17 V');
    expect(headline({ typ: 650, unit: 'kHz' })).toBe('650 kHz');
    expect(headline({ max: 0.6, unit: 'V' })).toBe('≤ 0.6 V');
  });

  it('formats relative times', () => {
    const now = Date.parse('2026-01-01T12:00:00Z');
    expect(relTime('2026-01-01T11:58:00Z', now)).toBe('2m ago');
    expect(relTime('2026-01-01T11:59:58Z', now)).toBe('just now');
  });

  it('escapes search snippets and marks matches', () => {
    expect(snippetHTML('a «VFB» <b>')).toBe('a <mark>VFB</mark> &lt;b&gt;');
  });
});

import { rangeText, toFilters } from '../views/CategoryView';

describe('category comparison', () => {
  it('formats compared values', () => {
    expect(rangeText({ min: 4.5, max: 17, unit: 'V', page: 0, paramId: 1, source: 'auto' })).toBe('4.5 V … 17 V');
    expect(rangeText({ typ: 650e3, unit: 'Hz', page: 0, paramId: 1, source: 'auto' })).toBe('650 kHz');
    expect(rangeText({ min: 1.33, typ: 1.45, max: 1.6, unit: 'A', page: 0, paramId: 1, source: 'auto' })).toBe('1.33 A / 1.45 A / 1.6 A');
  });

  it('parses typed filters with units and prefixes', () => {
    expect(toFilters({ vin: { op: '>=', text: '24 V' }, iq: { op: '<=', text: '1µ' }, fsw: { op: '>=', text: '' }, x: { op: '>=', text: 'abc' } })).toEqual([
      { key: 'vin', op: '>=', value: 24 },
      { key: 'iq', op: '<=', value: 1e-6 },
    ]);
  });
});

describe('plural', () => {
  it('pluralizes counts', async () => {
    const { plural } = await import('./format');
    expect(plural(1, 'part')).toBe('1 part');
    expect(plural(0, 'part')).toBe('0 parts');
    expect(plural(2522372, 'part')).toBe(`${(2522372).toLocaleString()} parts`);
  });
});
