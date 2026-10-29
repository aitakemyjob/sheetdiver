import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { fakeBackend, recorded, type FakeBackend } from '../../test/backend';
import type { SimRun, SimulationData, SimulationDetail } from '../../lib/types';
import { ProjectView } from '../ProjectView';
import { SettingsView } from '../SettingsView';

let b: FakeBackend;
beforeEach(() => {
  b = fakeBackend();
});
afterEach(cleanup);

const plots = [{ id: 'tran1', name: 'Transient Analysis', points: 3, vectors: [{ name: 'time', kind: 'time' }, { name: 'v(out)', kind: 'voltage' }, { name: 'v(vin)', kind: 'voltage' }] }];

/** A run of a variation in a condition, with its final voltage. */
const run = (id: number, variation: string, condition: string, vfinal: number): SimRun => ({
  id, variationId: variation === 'Rev A' ? 1 : 2, variation, condition, state: 'done', ngspice: '42', tookMs: 4, problems: [], measures: [{ name: 'vfinal', value: vfinal }], plots,
});

/** The recorded test with both variations run: Rev A sags below its target in both conditions; Rev B, after the schematic change, passes. */
function ran(): SimulationDetail {
  const d = structuredClone(recorded('sim.get', { project: 'Sensor board', name: '3V3 start-up' })) as SimulationDetail;
  const values: Record<string, number> = { 'Rev A/room': 3.1, 'Rev A/hot': 3.0, 'Rev B/room': 3.3, 'Rev B/hot': 3.25 };
  let id = 10;
  for (const v of d.variations) {
    v.latest = ['room', 'hot'].map((c) => run(++id, v.variation.name, c, values[`${v.variation.name}/${c}`]));
    v.checks = v.latest.map((r) => ({ measure: 'vfinal', min: 3.2, unit: 'V', variation: r.variation, condition: r.condition, value: r.measures[0].value, pass: r.measures[0].value! >= 3.2 }));
  }
  d.runs = d.variations.flatMap((v) => v.latest);
  return d;
}

/** Chart data for a run: its variation's final voltage. */
function data(args: unknown): SimulationData {
  const { run: id } = args as { run: number };
  const r = ran().runs.find((x) => x.id === id)!;
  const top = r.measures[0].value!;
  return { run: id, variation: r.variation, condition: r.condition, plot: 'tran1', xName: 'time', xUnit: 's', series: [{ name: 'v(out)', unit: 'V', x: [0, 1e-3, 5e-3], y: [0, 2, top] }, { name: 'v(vin)', unit: 'V', x: [0, 1e-3, 5e-3], y: [0, 12, 12] }] };
}

describe('the Simulations tab', () => {
  it('shows a test across its variations: the targets as a matrix, the history, and a chart comparing them', async () => {
    b.on('sim.get', () => ran());
    b.on('sim.data', data);
    render(<ProjectView params={{ project: 'Sensor board', tab: 'simulations' }} />);
    const nav = await screen.findByRole('navigation', { name: 'Simulations' });
    expect(within(nav).getByRole('button', { name: /3V3 start-up.*2 variations · latest Rev B/ })).toBeTruthy();
    expect(await screen.findByText('settles within', { exact: false })).toBeTruthy();
    // The target as a matrix: Rev A fails in both conditions, Rev B passes in both.
    const matrix = screen.getByRole('table', { name: 'Target vfinal' });
    const rows = within(matrix).getAllByRole('row').slice(1).map((r) => r.textContent);
    expect(rows[0]).toMatch(/Rev A.*first draft.*3\.1 V.*fail.*3 V.*fail/);
    expect(rows[1]).toMatch(/Rev B.*C1 up to 22 µF.*3\.3 V.*pass.*3\.25 V.*pass/);
    // The history, latest first, with what changed and the agent's note.
    const history = screen.getByRole('region', { name: 'Variations' });
    const cards = within(history).getAllByLabelText(/^Variation /);
    expect(cards.map((c) => c.getAttribute('aria-label'))).toEqual(['Variation Rev B', 'Variation Rev A']);
    expect(within(cards[1]).getByText('Sags when the radio wakes.')).toBeTruthy();
    // The chart compares the variations the agent named, in its condition: two signals each.
    const chart = await screen.findByRole('img', { name: 'Start-up' });
    expect(chart.querySelectorAll('path').length).toBe(4);
    const legend = screen.getByRole('list', { name: 'Start-up legend' });
    expect(within(legend).getAllByRole('listitem').map((l) => l.textContent)).toEqual(['v(out) · Rev A', 'v(vin) · Rev A', 'v(out) · Rev B', 'v(vin) · Rev B']);
    // Only the room runs were asked for.
    const asked = () => b.callsTo('sim.data').map((a) => (a as { run: number }).run).sort();
    expect(asked()).toEqual([11, 13]);
    // Leaving Rev A out of the comparison redraws with Rev B alone.
    fireEvent.click(within(screen.getByRole('group', { name: 'Start-up: variations' })).getByRole('button', { name: 'Rev A' }));
    await waitFor(() => expect(screen.getByRole('img', { name: 'Start-up' }).querySelectorAll('path').length).toBe(2));
    // Its values as a table.
    fireEvent.click(screen.getByRole('button', { name: 'Table' }));
    expect(screen.getByRole('table', { name: 'Chart values' })).toBeTruthy();
  });

  it('runs a variation again, or deletes it', async () => {
    b.on('sim.get', () => ran());
    b.on('sim.ngspice', () => ({ path: '/usr/bin/ngspice', version: '42', from: 'PATH' }));
    b.on('sim.data', data);
    render(<ProjectView params={{ project: 'Sensor board', tab: 'simulations' }} />);
    await screen.findByText(/ngspice 42/);
    const revA = await screen.findByLabelText('Variation Rev A');
    const again = within(revA).getByRole('button', { name: /Run again/ });
    await waitFor(() => expect((again as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(again);
    await waitFor(() => expect(b.callsTo('sim.run')).toEqual([{ project: 'Sensor board', name: '3V3 start-up', variation: 'Rev A', conditions: [] }]));
    window.confirm = () => true;
    fireEvent.click(within(revA).getByTitle(/Delete Rev A/));
    await waitFor(() => expect(b.callsTo('sim.deleteVariation')).toEqual([{ project: 'Sensor board', name: '3V3 start-up', variation: 'Rev A' }]));
  });

  it('says what to do when there are none, and whether ngspice is there', async () => {
    b.on('sim.list', () => []);
    b.on('sim.ngspice', () => ({ error: 'ngspice wasn’t found: install it' }));
    render(<ProjectView params={{ project: 'Empty board', tab: 'simulations' }} />);
    expect(await screen.findByText('No simulations yet')).toBeTruthy();
    expect(await screen.findByText(/ngspice wasn’t found/)).toBeTruthy();
  });
});

describe('the simulation settings', () => {
  it('shows the ngspice found and saves a path', async () => {
    b.on('sim.ngspice', () => ({ path: '/usr/bin/ngspice', version: '42', from: 'PATH' }));
    render(<SettingsView />);
    expect(await screen.findByText(/ngspice 42 at \/usr\/bin\/ngspice \(on the PATH\)/)).toBeTruthy();
    const input = screen.getByRole('textbox', { name: 'ngspice path' });
    fireEvent.change(input, { target: { value: 'C:\\Spice64\\bin\\ngspice_con.exe' } });
    fireEvent.blur(input);
    await waitFor(() => expect((b.callsTo('settings.update').at(-1) as { ngspicePath?: string } | undefined)?.ngspicePath).toBe('C:\\Spice64\\bin\\ngspice_con.exe'));
  });
});
