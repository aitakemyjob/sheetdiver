import { describe, expect, it } from 'vitest';
import { catalogProgressText, sizeText, useStore } from './store';
import { priceText } from '../components/CatalogPanel';
import type { CatalogStatus } from './types';

const status = (p: Partial<CatalogStatus>): CatalogStatus => ({
  installed: true, updating: false, parts: 2522372, inStock: 438338, published: '2026-10-18T22:15:48Z', downloaded: '2026-10-19T04:00:00Z',
  sizeBytes: 1397583872, source: 'x', ageDays: 0, remindDays: 14, stale: false, ...p,
});

describe('parts catalogue', () => {
  it('formats prices and sizes', () => {
    expect(priceText(0)).toBe('–');
    expect(priceText(1.0261)).toBe('$1.03');
    expect(priceText(0.0895)).toBe('$0.0895');
    expect(priceText(0.05)).toBe('$0.05');
    expect(sizeText(1397583872)).toBe('1.3 GB');
    expect(sizeText(209715200)).toBe('200 MB');
  });

  it('describes download progress', () => {
    expect(catalogProgressText(undefined)).toBe('');
    expect(catalogProgressText(status({ progress: { stage: 'downloading', done: 50 << 20, total: 200 << 20 } }))).toBe('Downloading 25% of 200 MB');
    expect(catalogProgressText(status({ progress: { stage: 'extracting', done: 1, total: 2 } }))).toBe('Unpacking 50%');
    expect(catalogProgressText(status({ progress: { stage: 'starting', done: 0, total: 0 } }))).toBe('Starting');
  });

  it('announces a finished or failed download', () => {
    const st = useStore.getState;
    useStore.setState({ catalog: status({ updating: true, installed: false }), toasts: [] });
    st().handleEvent({ type: 'catalog', time: '', data: status({}) });
    expect(st().toasts.at(-1)?.title).toBe('Parts catalogue ready');
    expect(st().toasts.at(-1)?.body).toBe(`${(2522372).toLocaleString()} parts, ${(438338).toLocaleString()} in stock`);
    useStore.setState({ catalog: status({ updating: true }) });
    st().handleEvent({ type: 'catalog', time: '', data: status({ error: 'republished' }) });
    expect(st().toasts.at(-1)?.title).toBe('Catalogue download failed');
    // Progress updates don't toast.
    const n = st().toasts.length;
    st().handleEvent({ type: 'catalog', time: '', data: status({ updating: true, progress: { stage: 'downloading', done: 1, total: 2 } }) });
    expect(st().toasts.length).toBe(n);
    expect(st().catalog?.progress?.stage).toBe('downloading');
  });
});

describe('catalogue categories', () => {
  it('groups subcategories, filters and hides empty ones', async () => {
    const { groupCategories } = await import('../views/CatalogView');
    const cats = [
      { category: 'Resistors', subcategory: 'Chip Resistor - Surface Mount', parts: 150, inStock: 40 },
      { category: 'Resistors', subcategory: 'Through Hole Resistors', parts: 20, inStock: 0 },
      { category: 'Transistors/Thyristors', subcategory: 'MOSFETs', parts: 90, inStock: 30 },
      { category: '', subcategory: 'Misc', parts: 5, inStock: 1 },
    ];
    const all = groupCategories(cats, '', false);
    expect(all.map((g) => g.category)).toEqual(['Resistors', 'Transistors/Thyristors', 'Other']);
    expect(all[0].subs.length).toBe(2);
    const stocked = groupCategories(cats, '', true);
    expect(stocked[0].subs.map((x) => x.subcategory)).toEqual(['Chip Resistor - Surface Mount']);
    expect(stocked[0].inStock).toBe(40);
    expect(groupCategories(cats, 'mosfet', false).map((g) => g.category)).toEqual(['Transistors/Thyristors']);
    expect(groupCategories(cats, 'resistors', false)[0].subs.length).toBe(2);
  });
});

describe('catalogue parameters', () => {
  it('picks columns and active filters', async () => {
    const { activeFilters, paramColumns, paramRange } = await import('../views/CatalogView');
    const params = [
      { key: 'Drain to Source Voltage', kind: 'number' as const, count: 9, unit: 'V', min: 8, max: 2500 },
      { key: 'Type', kind: 'text' as const, count: 9, values: [{ value: 'N-Channel', count: 6 }] },
      { key: 'Operating Temperature', kind: 'number' as const, count: 8, unit: '℃', min: -55, max: 175 },
      { key: 'RDS(on)', kind: 'number' as const, count: 8, unit: 'Ω', min: 0.00035, max: 850 },
      { key: 'Pd - Power Dissipation', kind: 'number' as const, count: 7, unit: 'W', min: 0.1, max: 1600 },
      { key: 'Gate Charge(Qg)', kind: 'number' as const, count: 6, unit: 'C', min: 1e-9, max: 1e-6 },
    ];
    expect(paramColumns(params, [], '')).toEqual(['Drain to Source Voltage', 'RDS(on)', 'Pd - Power Dissipation', 'Gate Charge(Qg)']);
    const fs = [
      { key: 'Type', op: '=' as const, input: '', values: ['N-Channel'] },
      { key: 'RDS(on)', op: '<=' as const, input: '' },
      { key: 'Package', op: '=' as const, input: '', values: [] },
    ];
    expect(activeFilters(fs)).toEqual([fs[0]]);
    expect(paramColumns(params, activeFilters(fs), 'attr:Gate Charge(Qg)').slice(0, 2)).toEqual(['Type', 'Gate Charge(Qg)']);
    // The package has its own column.
    expect(paramColumns(params, [{ key: 'Package', op: '=', input: '', values: ['SOT-23'] }], '')).not.toContain('Package');
    expect(paramRange(params[0])).toBe('8 V … 2.5 kV');
    expect(paramRange(params[1])).toBe('');
  });
});

describe('value picker labels', () => {
  it('summarizes picked values', async () => {
    const { valuesLabel } = await import('../views/CatalogView');
    expect(valuesLabel([])).toBe('any');
    expect(valuesLabel(['SOT-23'])).toBe('SOT-23');
    expect(valuesLabel(['SOT-23', 'SOT-23-3'])).toBe('SOT-23, SOT-23-3');
    expect(valuesLabel(['SOT-23', 'SOT-23-3', 'TSOT-23-6'])).toBe('SOT-23 +2');
  });
});
