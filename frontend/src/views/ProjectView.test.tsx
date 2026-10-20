import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { fakeBackend, type FakeBackend } from '../test/backend';
import { currentHint, currentTitle, loadText, parseLoad, ProjectView } from './ProjectView';
import type { ProjectPart } from '../lib/types';

let b: FakeBackend;
beforeEach(() => {
  b = fakeBackend();
});

describe('ProjectView', () => {
  it('shows an empty project', async () => {
    render(<ProjectView params={{ project: 'Empty board' }} />);
    expect(await screen.findByText('No parts yet')).toBeInTheDocument();
    expect(screen.queryByText(/checks pass/)).not.toBeInTheDocument();
  });

  it('shows rails, the BOM, checks and saved calculations', async () => {
    render(<ProjectView params={{ project: 'Sensor board' }} />);
    expect(await screen.findByRole('heading', { name: /^Sensor board/ })).toBeInTheDocument();
    for (const t of ['TPX5430', 'SD1235', 'VIN', '3V3', 'ADC divider']) expect(screen.getAllByText(t).length).toBeGreaterThan(0);
    expect(screen.getByText(/Checks against the datasheets/i)).toBeInTheDocument();
  });

  it('adds a rail', async () => {
    render(<ProjectView params={{ project: 'Sensor board' }} />);
    await screen.findByRole('heading', { name: /^Sensor board/ });
    fireEvent.change(screen.getByPlaceholderText('Rail (3V3)'), { target: { value: '1V8' } });
    fireEvent.change(screen.getByPlaceholderText('V nom'), { target: { value: '1.8' } });
    fireEvent.click(screen.getByRole('button', { name: /Add rail/ }));
    await waitFor(() => expect(b.callsTo('rails.set')).toHaveLength(1));
    expect(b.callsTo('rails.set')[0]).toMatchObject({ name: '1V8', vNom: 1.8 });
  });
});

describe('rail loads and ripple', () => {
  it("shows each rail's ripple budget and other loads", async () => {
    render(<ProjectView params={{ project: 'Sensor board' }} />);
    await screen.findByRole('heading', { name: /^Sensor board/ });
    expect(screen.getByText(/other loads 50 mA/)).toBeInTheDocument();
    // VIN 12 V for a 4.5 V-minimum part: 7.5 V each way.
    expect(screen.getByText('15 V p-p')).toBeInTheDocument();
    // 3V3 can't carry any: a part on it needs 4.5 V.
    const none = screen.getByText('0 V p-p').closest('div')!;
    expect(none).toHaveClass('err-text');
    expect(none).toHaveTextContent(/minimum input 4.5 V/);
  });

  it('saves other loads with a rail', async () => {
    render(<ProjectView params={{ project: 'Sensor board' }} />);
    await screen.findByRole('heading', { name: /^Sensor board/ });
    fireEvent.change(screen.getByPlaceholderText('Rail (3V3)'), { target: { value: '1V8' } });
    fireEvent.change(screen.getByPlaceholderText('Other loads (A)'), { target: { value: '100m/150m' } });
    fireEvent.click(screen.getByRole('button', { name: /Add rail/ }));
    await waitFor(() => expect(b.callsTo('rails.set')[0]).toMatchObject({ name: '1V8', otherTyp: 0.1, otherMax: 0.15 }));
  });

  it('edits a rail with its other loads', async () => {
    render(<ProjectView params={{ project: 'Sensor board' }} />);
    await screen.findByRole('heading', { name: /^Sensor board/ });
    fireEvent.click(screen.getByText(/other loads 50 mA/).closest('.rail-card')!);
    expect(screen.getByPlaceholderText('Other loads (A)')).toHaveValue('0.05');
    expect(screen.getByRole('button', { name: /Update rail/ })).toBeInTheDocument();
  });

  it('reads and writes other-load entries', () => {
    expect(parseLoad('100m')).toEqual([0.1, 0.1]);
    expect(parseLoad('100m/150m')).toEqual([0.1, 0.15]);
    expect(parseLoad('')).toEqual([undefined, undefined]);
    expect(loadText(0.1, 0.15)).toBe('0.1/0.15');
    expect(loadText(0.05, 0.05)).toBe('0.05');
    expect(loadText(undefined, 0.2)).toBe('0.2');
    expect(loadText(null, null)).toBe('');
  });
});

describe('BOM current cells', () => {
  const pp = (p: Partial<ProjectPart>) => ({ id: 1, projectId: 1, partId: 1, mpn: 'X', category: '', hasDatasheet: true, qty: 1, ...p }) as ProjectPart;
  it('say what an empty cell means', () => {
    expect(currentHint(pp({ supplyRail: '3V3' }))).toBe('');
    expect(currentHint(pp({ supplyRail: '4.2V', outputRail: '3V3', category: 'buck-boost' }))).toBe('derived');
    expect(currentHint(pp({ supplyRail: '5V', outputRail: '4.2V', category: 'charger' }))).toBe('charge A');
    expect(currentHint(pp({ supplyRail: '4.2V', outputRail: '4.2V', category: 'battery-protection' }))).toBe('');
    expect(currentTitle(pp({ supplyRail: '5V', outputRail: '4.2V', category: 'charger' }))).toMatch(/charge current your board programs/);
    expect(currentTitle(pp({ supplyRail: '4.2V', outputRail: '3V3' }))).toMatch(/input current/);
    expect(currentTitle(pp({ supplyRail: '3V3' }))).toMatch(/draws from its supply rail/);
  });
});
