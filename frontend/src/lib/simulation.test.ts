import { describe, expect, it } from 'vitest';
import { axisName, chartModel, endLabels, logTicks, nearestIndex, niceTicks, panels, readout, runsFor, stateBadge, targetRows } from './simulation';
import type { SimRun, SimulationData } from './types';

const run = (id: number, variant: string, state: SimRun['state'] = 'done'): SimRun => ({
  id, simulationId: 1, variant, state, tookMs: 5, problems: [], measures: [],
  plots: [{ id: 'tran1', name: 'Transient Analysis', points: 3, vectors: [{ name: 'time', kind: 'time' }, { name: 'v(out)', kind: 'voltage' }] }],
});

describe('simulation charts', () => {
  it('splits an AC chart into magnitude and phase, each on its own axis', () => {
    const ps = panels([{ title: 'Step', plot: 'tran1', signals: ['v(out)'] }, { title: 'Loop', plot: 'ac1', signals: ['v(out)'] }, { title: 'Phase only', plot: 'ac1', signals: ['v(out)'], part: 'phase' }]);
    expect(ps.map((p) => [p.title, p.part])).toEqual([
      ['Step', ''],
      ['Loop — magnitude', 'mag'],
      ['Loop — phase', 'phase'],
      ['Phase only', 'phase'],
    ]);
  });

  it('draws the latest run of each variant asked for that has results', () => {
    const latest = [run(1, 'room'), run(2, 'hot'), run(3, 'cold', 'failed')];
    expect(runsFor({ title: '', plot: 'tran1', signals: [] }, latest).map((r) => r.variant)).toEqual(['room', 'hot']);
    expect(runsFor({ title: '', plot: 'tran1', signals: [], variants: ['HOT'] }, latest).map((r) => r.variant)).toEqual(['hot']);
    expect(runsFor({ title: '', plot: 'ac1', signals: [] }, latest)).toEqual([]);
  });

  it('picks round ticks, and decades on a log axis', () => {
    expect(niceTicks(0, 5e-3, 5)).toEqual([0, 0.001, 0.002, 0.003, 0.004, 0.005]);
    expect(niceTicks(-0.2, 3.5, 4)).toEqual([0, 1, 2, 3]);
    expect(logTicks(10, 1e5)).toEqual([10, 100, 1000, 10000, 100000]);
  });

  it('builds a chart: a line per signal and variant, log frequency for AC, values under the cursor', () => {
    const data = (run: number, variant: string): SimulationData => ({
      run, variant, plot: 'ac1', xName: 'frequency', xUnit: 'Hz',
      series: [{ name: 'v(out)', unit: 'dB', x: [10, 100, 1000], y: [0, -3, -20] }],
    });
    const [mag] = panels([{ title: 'Response', plot: 'ac1', signals: ['v(out)'] }]);
    const m = chartModel(mag, [data(1, 'room'), data(2, 'hot')], false);
    expect(m.xLog).toBe(true);
    expect(m.series.map((s) => s.label)).toEqual(['v(out) · room', 'v(out) · hot']);
    expect(m.series.map((s) => s.short)).toEqual(['room', 'hot']); // one signal: its ends say which variant
    expect(m.series[0].color).not.toBe(m.series[1].color);
    expect(m.yLabel).toBe('dB');
    expect(m.xTicks.map((t) => t.label)).toEqual(['10 Hz', '100 Hz', '1 kHz']);
    expect(readout(m, 110).rows[0].value).toBe('-3 dB');
    expect(nearestIndex([1, 2, 3, 10], 8)).toBe(3);
  });

  it('lays out targets by measurement, with each variant’s value and verdict', () => {
    const rows = targetRows([
      { measure: 'vfinal', min: 3.2, unit: 'V', variant: 'room', value: 3.3, pass: true },
      { measure: 'vfinal', min: 3.2, unit: 'V', variant: 'hot', value: 3.1, pass: false },
      { measure: 'ripple', max: 0.05, unit: 'V', variant: 'room' },
    ]);
    expect(rows.map((r) => r.range)).toEqual(['≥ 3.2 V', '≤ 50 mV']);
    expect(rows[0].results).toEqual([
      { variant: 'room', value: '3.3 V', pass: true },
      { variant: 'hot', value: '3.1 V', pass: false },
    ]);
    expect(rows[1].results[0].value).toBe('–');
  });

  it('names a DC sweep’s axis after its source', () => {
    const an = [{ kind: 'tran', line: '.tran 1u 1m' }, { kind: 'dc', line: '.dc Vin 2 6 0.05' }];
    expect(axisName('v(v-sweep)', 'dc1', an)).toBe('Vin (swept)');
    expect(axisName('time', 'tran1', an)).toBe('time');
  });

  it('spreads the end labels apart, or drops them when they can’t fit', () => {
    expect(endLabels([100, 101, 102], 20, 220)).toEqual([100, 112, 124]);
    expect(endLabels([219, 220], 20, 220)).toEqual([208, 220]);
    expect(endLabels([50, 50, 50], 40, 50)).toEqual([null, null, null]);
  });

  it('says what a run state means', () => {
    expect(stateBadge('done')).toEqual({ tone: 'ok', label: 'done' });
    expect(stateBadge('stale').label).toContain('changed');
    expect(stateBadge('failed').tone).toBe('err');
  });
});
