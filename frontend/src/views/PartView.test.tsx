import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { useStore } from '../lib/store';
import { fakeBackend, type FakeBackend } from '../test/backend';
import { PartView } from './PartView';

let b: FakeBackend;
beforeEach(() => {
  b = fakeBackend();
});

/** Renders a part and waits for it to load. */
async function part(mpn = 'SD1234', params: Record<string, unknown> = {}) {
  render(<PartView params={{ part: mpn, ...params }} />);
  await screen.findByRole('heading', { name: new RegExp(mpn) });
}

/** Opens a sub-tab by its label. */
function tab(label: string) {
  fireEvent.click(screen.getByRole('button', { name: new RegExp(`^${label}`) }));
}

describe('PartView', () => {
  it('shows the overview: key parameters, datasheet and outline', async () => {
    await part();
    expect(screen.getByText('Key parameters', { exact: false })).toBeInTheDocument();
    expect(screen.getAllByText(/Absolute Maximum Ratings/).length).toBeGreaterThan(0);
    // English datasheets have no Translate button.
    expect(screen.queryByRole('button', { name: /Translate/ })).not.toBeInTheDocument();
    // Sent from an effect after the render: waited for, or it flakes under load.
    await waitFor(() => expect(b.callsTo('ui.context')[0]).toMatchObject({ view: 'part/overview', part: 'SD1234' }));
  });

  it('lists parameters', async () => {
    await part();
    tab('Parameters');
    expect(await screen.findByDisplayValue('Switching frequency')).toBeInTheDocument();
    expect(b.callsTo('parameters.list').at(-1)).toMatchObject({ datasheetId: 1, mpn: 'SD1234' });
  });

  it('lists figures and pins', async () => {
    await part();
    tab('Figures');
    expect((await screen.findAllByText(/Efficiency vs Output Current/)).length).toBeGreaterThan(0);
    tab('Pins');
    const table = await screen.findByRole('table');
    for (const pin of ['GND', 'SW', 'VIN', 'VFB']) expect(within(table).getByText(pin)).toBeInTheDocument();
  });

  it('reads a section as formatted text', async () => {
    await part();
    tab('Sections');
    fireEvent.click((await screen.findAllByText(/Electrical Characteristics/)).at(-1)!);
    await waitFor(() => expect(b.callsTo('section.get').length).toBeGreaterThan(0));
  });

  it('lists tables, notes and variants', async () => {
    await part();
    tab('Tables');
    expect(await screen.findByText(/Output Voltage Settings/)).toBeInTheDocument();
    tab('Notes');
    await waitFor(() => expect(b.callsTo('notes.list').length).toBeGreaterThan(0));
    tab('Variants');
    expect(await screen.findByText('SD1234DDCR')).toBeInTheDocument();
  });

  it('finds alternatives', async () => {
    await part();
    tab('Alternatives');
    await waitFor(() => expect(b.callsTo('part.alternatives').length).toBeGreaterThan(0));
  });

  it('re-extracts an English datasheet directly', async () => {
    await part();
    fireEvent.click(screen.getByTitle(/Redo automatic extraction/));
    await waitFor(() => expect(b.callsTo('datasheet.reextract')).toEqual([{ id: 1, translated: false }]));
    expect(useStore.getState().toasts.at(-1)?.title).toMatch(/Re-extracted/);
  });

  it('asks before deleting', async () => {
    await part();
    fireEvent.click(screen.getByTitle(/Delete this datasheet/));
    const dialog = await screen.findByRole('dialog');
    expect(await within(dialog).findByText(/SD1235/)).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    expect(b.callsTo('delete.apply')).toEqual([]);
  });

  it('opens on a requested sub-tab', async () => {
    await part('SD1234', { tab: 'pins' });
    expect(await screen.findByRole('table')).toBeInTheDocument();
  });

  it('shows a part from another datasheet', async () => {
    await part('TPX5430');
    expect(screen.getAllByText(/TPX5430/).length).toBeGreaterThan(0);
  });
});
