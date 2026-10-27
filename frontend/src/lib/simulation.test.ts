import { describe, expect, it } from 'vitest';
import { axisName, chartModel, defaultVariations, endLabels, logTicks, nearestIndex, niceTicks, panels, readout, runsFor, stateBadge, targetMatrix } from './simulation';
import type { SimRun, SimulationData, SimulationDetail, VariationDetail } from './types';

const run = (id: number, variation: string, condition: string, state: SimRun['state'] = 'done'): SimRun => ({
  id, variationId: 1, variation, condition, state, tookMs: 5, problems: [], measures: [],
  plots: [{ id: 'tran1', name: 'Transient Analysis', points: 3, vectors: [{ name: 'time', kind: 'time' }, { name: 'v(out)', kind: 'voltage' }] }],
});

/** A variation with its latest runs. */
const variation = (name: string, latest: SimRun[]): VariationDetail => ({
  variation: { id: 1, simulationId: 1, name, netlist: '', models: [], conditions: [], source: 'agent', created: '', updated: '' },
  analyses: [], measures: [], latest, checks: [],
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

  it('compares the variations asked for (else the latest), in the conditions asked for', () => {
    const vs = [variation('Rev A', [run(1, 'Rev A', 'room'), run(2, 'Rev A', 'hot')]), variation('Rev B', [run(3, 'Rev B', 'room'), run(4, 'Rev B', 'hot', 'failed')])];
    const spec = { title: '', plot: 'tran1', signals: [] };
    expect(defaultVariations(spec, vs)).toEqual(['Rev B']);
    expect(defaultVariations({ ...spec, variations: ['rev a', 'Rev Z'] }, vs)).toEqual(['rev a']);
    expect(runsFor(spec, vs, ['Rev B']).map((r) => r.id)).toEqual([3]); // the failed run has nothing to draw
    expect(runsFor({ ...spec, conditions: ['ROOM'] }, vs, ['Rev A', 'Rev B']).map((r) => r.id)).toEqual([1, 3]);
    expect(runsFor({ ...spec, plot: 'ac1' }, vs, ['Rev A'])).toEqual([]);
  });

  it('picks round ticks, and decades on a log axis', () => {
    expect(niceTicks(0, 5e-3, 5)).toEqual([0, 0.001, 0.002, 0.003, 0.004, 0.005]);
    expect(niceTicks(-0.2, 3.5, 4)).toEqual([0, 1, 2, 3]);
    expect(logTicks(10, 1e5)).toEqual([10, 100, 1000, 10000, 100000]);
  });

  it('builds a chart: a line per signal and variant, log frequency for AC, values under the cursor', () => {
    const data = (run: number, variation: string): SimulationData => ({
      run, variation, condition: 'room', plot: 'ac1', xName: 'frequency', xUnit: 'Hz',
      series: [{ name: 'v(out)', unit: 'dB', x: [10, 100, 1000], y: [0, -3, -20] }],
    });
    const [mag] = panels([{ title: 'Response', plot: 'ac1', signals: ['v(out)'] }]);
    const m = chartModel(mag, [data(1, 'Rev A'), data(2, 'Rev B')], false);
    expect(m.xLog).toBe(true);
    // Lines are named by what tells them apart: the condition is the same, so only the variation.
    expect(m.series.map((s) => s.label)).toEqual(['v(out) · Rev A', 'v(out) · Rev B']);
    expect(m.series.map((s) => s.short)).toEqual(['Rev A', 'Rev B']);
    expect(m.series[0].color).not.toBe(m.series[1].color);
    expect(m.yLabel).toBe('dB');
    expect(m.xTicks.map((t) => t.label)).toEqual(['10 Hz', '100 Hz', '1 kHz']);
    expect(readout(m, 110).rows[0].value).toBe('-3 dB');
    expect(nearestIndex([1, 2, 3, 10], 8)).toBe(3);
  });

  it('lays out each target as a matrix of variations against conditions', () => {
    const d: SimulationDetail = {
      simulation: { id: 1, projectId: 1, name: 't', source: 'agent', created: '', updated: '', presentation: { summary: '', plots: [], checks: [{ measure: 'vfinal', min: 3.2, unit: 'V' }, { measure: 'ripple', max: 0.05, unit: 'V' }] } },
      runs: [],
      variations: [
        { ...variation('Rev A', [run(1, 'Rev A', 'room'), run(2, 'Rev A', 'hot')]), checks: [
          { measure: 'vfinal', min: 3.2, unit: 'V', variation: 'Rev A', condition: 'room', value: 3.1, pass: false },
          { measure: 'vfinal', min: 3.2, unit: 'V', variation: 'Rev A', condition: 'hot', value: 3.0, pass: false },
        ] },
        { ...variation('Rev B', [run(3, 'Rev B', 'room')]), checks: [{ measure: 'vfinal', min: 3.2, unit: 'V', variation: 'Rev B', condition: 'room', value: 3.3, pass: true }] },
      ],
    };
    const [vfinal, ripple] = targetMatrix(d);
    expect([vfinal.range, ripple.range]).toEqual(['≥ 3.2 V', '≤ 50 mV']);
    expect(vfinal.rows.map((r) => [r.variation, ...r.cells.map((c) => `${c.condition} ${c.value} ${c.pass}`)])).toEqual([
      ['Rev A', 'room 3.1 V false', 'hot 3 V false'],
      ['Rev B', 'room 3.3 V true', 'hot  undefined'], // Rev B wasn't run hot
    ]);
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
