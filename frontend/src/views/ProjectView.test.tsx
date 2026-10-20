import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { fakeBackend, type FakeBackend } from '../test/backend';
import { ProjectView } from './ProjectView';

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
