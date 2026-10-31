import { describe, expect, it } from 'vitest';
import { dimLabel, limitCell, packageTitle, recordedRows, sourceBadge } from './packages';
import type { PartPackage } from './types';

const pkg: PartPackage = {
  name: 'SOT-23',
  code: 'DDC0006A',
  reference: 'MO-193',
  page: 32,
  dims: [{ symbol: 'e', key: 'pitch', nom: 0.95, source: 'auto' }],
  land: [],
  sources: ['agent', 'auto'],
  rows: [
    { id: 1, name: 'SOT-23', source: 'agent', updated: '' },
    { id: 2, name: 'SOT-23', source: 'auto', updated: '' },
  ],
};

describe('package dimensions', () => {
  it('labels and formats dimensions', () => {
    expect(dimLabel(pkg.dims[0])).toBe('Pitch');
    expect(dimLabel({ symbol: 'H', description: 'Lead span', source: 'auto' })).toBe('Lead span');
    expect(limitCell(undefined)).toBe('–');
    expect(limitCell(1.23456789)).toBe('1.2346');
  });
  it('names the package and its recorded rows', () => {
    expect(packageTitle(pkg)).toBe('SOT-23 (DDC0006A) JEDEC MO-193');
    expect(recordedRows(pkg).map((r) => r.id)).toEqual([1]);
    expect(sourceBadge('auto')).toBeNull();
    expect(sourceBadge('agent')?.label).toBe('Agent');
  });
});
