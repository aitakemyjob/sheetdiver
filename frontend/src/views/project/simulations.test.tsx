import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { fakeBackend, recorded, type FakeBackend } from '../../test/backend';
import type { SimulationDetail } from '../../lib/types';
import { ProjectView } from '../ProjectView';
import { SettingsView } from '../SettingsView';

let b: FakeBackend;
beforeEach(() => {
  b = fakeBackend();
});
afterEach(cleanup);

/** The recorded simulation with its two variants run: room passes its target, hot fails it. */
function ran(): SimulationDetail {
  const d = structuredClone(recorded('sim.get', { project: 'Sensor board', name: '3V3 start-up' })) as SimulationDetail;
  const plots = [{ id: 'tran1', name: 'Transient Analysis', points: 3, vectors: [{ name: 'time', kind: 'time' }, { name: 'v(out)', kind: 'voltage' }, { name: 'v(vin)', kind: 'voltage' }] }];
  d.latest = [
    { id: 11, simulationId: 1, variant: 'room', state: 'done', ngspice: '42', tookMs: 4, problems: [], measures: [{ name: 'vfinal', value: 3.3 }], plots },
    { id: 12, simulationId: 1, variant: 'hot', state: 'done', ngspice: '42', tookMs: 5, problems: [], measures: [{ name: 'vfinal', value: 3.1 }], plots },
  ];
  d.runs = d.latest;
  d.checks = [
    { measure: 'vfinal', min: 3.2, unit: 'V', why: "the sensor's minimum supply", variant: 'room', value: 3.3, pass: true },
    { measure: 'vfinal', min: 3.2, unit: 'V', why: "the sensor's minimum supply", variant: 'hot', value: 3.1, pass: false },
  ];
  return d;
}

describe('the Simulations tab', () => {
  it('lists the simulations and shows the agent’s write-up, targets and charts', async () => {
    b.on('sim.get', () => ran());
    b.on('sim.data', (a) => {
      const { run } = a as { run: number };
      const top = run === 11 ? 3.3 : 3.1;
      return { run, variant: run === 11 ? 'room' : 'hot', plot: 'tran1', xName: 'time', xUnit: 's', series: [{ name: 'v(out)', unit: 'V', x: [0, 1e-3, 5e-3], y: [0, 2, top] }, { name: 'v(vin)', unit: 'V', x: [0, 1e-3, 5e-3], y: [0, 12, 12] }] };
    });
    render(<ProjectView params={{ project: 'Sensor board', tab: 'simulations' }} />);
    const nav = await screen.findByRole('navigation', { name: 'Simulations' });
    expect(within(nav).getByRole('button', { name: /3V3 start-up/ })).toBeTruthy();
    expect(await screen.findByText('settles within', { exact: false })).toBeTruthy();
    // Targets say pass and fail in words, not colour alone.
    const targets = screen.getByRole('region', { name: 'Targets' });
    expect(within(targets).getByText('pass')).toBeTruthy();
    expect(within(targets).getByText('fail')).toBeTruthy();
    expect(within(targets).getByText('≥ 3.2 V')).toBeTruthy();
    // The chart: two signals for each of two variants, with a legend.
    const chart = await screen.findByRole('img', { name: 'Start-up' });
    expect(chart.querySelectorAll('path').length).toBe(4);
    const legend = screen.getByRole('list', { name: 'Start-up legend' });
    expect(within(legend).getAllByRole('listitem').map((l) => l.textContent)).toEqual(['v(out) · room', 'v(vin) · room', 'v(out) · hot', 'v(vin) · hot']);
    // Each run's data was asked for once, by run.
    expect(b.callsTo('sim.data').map((a) => (a as { run: number }).run).sort()).toEqual([11, 12]);
    // Its values as a table.
    fireEvent.click(screen.getByRole('button', { name: 'Table' }));
    expect(screen.getByRole('table', { name: 'Chart values' })).toBeTruthy();
  });

  it('runs a simulation again', async () => {
    b.on('sim.get', () => ran());
    b.on('sim.ngspice', () => ({ path: '/usr/bin/ngspice', version: '42', from: 'PATH' }));
    b.on('sim.data', () => ({ run: 11, variant: 'room', plot: 'tran1', xName: 'time', xUnit: 's', series: [] }));
    render(<ProjectView params={{ project: 'Sensor board', tab: 'simulations' }} />);
    const again = await screen.findByRole('button', { name: /Run again/ });
    await screen.findByText(/ngspice 42/);
    await waitFor(() => expect((again as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(again);
    await waitFor(() => expect(b.callsTo('sim.run')).toEqual([{ project: 'Sensor board', name: '3V3 start-up', variants: [] }]));
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
