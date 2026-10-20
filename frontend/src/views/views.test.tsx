import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { useStore } from '../lib/store';
import { fakeBackend, recorded, type FakeBackend } from '../test/backend';
import type { CalcResponse } from '../lib/types';
import { CalcView } from './CalcView';
import { CategoryView } from './CategoryView';
import { CatalogView } from './CatalogView';
import { FigureView } from './FigureView';
import { NotesPanel } from './NotesPanel';
import { PageView } from './PageView';
import { SearchView } from './SearchView';
import { SettingsView } from './SettingsView';
import { ViewHost } from './ViewHost';
import { Welcome } from './Welcome';

let b: FakeBackend;
beforeEach(() => {
  b = fakeBackend();
});

describe('SearchView', () => {
  it('searches and opens a hit', async () => {
    render(<SearchView params={{ query: 'switching frequency' }} />);
    await waitFor(() => expect(b.callsTo('search')[0]).toMatchObject({ query: 'switching frequency' }));
    const hit = (await screen.findAllByText(/Switching frequency/))[0];
    fireEvent.click(hit.closest('.search-hit') ?? hit);
    // A parameter opens its page with the row highlighted.
    await waitFor(() => expect(useStore.getState().tabs.at(-1)).toMatchObject({ kind: 'page', params: { ds: 1 } }));
  });
});

describe('CategoryView', () => {
  it('compares the parts of a type', async () => {
    render(<CategoryView params={{ category: 'buck', highlight: 'SD1234' }} />);
    await waitFor(() => expect(b.callsTo('compare')[0]).toMatchObject({ category: 'buck' }));
    for (const mpn of ['SD1234', 'SD1235', 'TPX5430']) expect((await screen.findAllByText(mpn)).length).toBeGreaterThan(0);
  });
});

describe('CalcView', () => {
  it('calculates as inputs change and shows outputs', async () => {
    b.on('calc.run', () => recorded('calc.run'));
    render(<CalcView params={{ kind: 'divider' }} />);
    await waitFor(() => expect(b.callsTo('calc.fields')[0]).toEqual({ kind: 'divider' }));
    expect((await screen.findAllByText(/Divider current/)).length).toBeGreaterThan(0);
    const vin = screen.getByText('vin').closest('.field')!.querySelector('input')!;
    fireEvent.change(vin, { target: { value: '5' } });
    await waitFor(() => expect(b.callsTo('calc.run').at(-1)).toMatchObject({ kind: 'divider', inputs: { vin: 5 } }));
  });

  it('shows a saved result', async () => {
    const result = recorded('calc.run') as CalcResponse;
    render(<CalcView params={{ kind: 'divider', result }} />);
    expect((await screen.findAllByText(/Ideal ratio/)).length).toBeGreaterThan(0);
  });

  it('switches calculator', async () => {
    render(<CalcView params={{ kind: 'buck' }} />);
    await waitFor(() => expect(b.callsTo('calc.fields')[0]).toEqual({ kind: 'buck' }));
    fireEvent.click(screen.getByRole('button', { name: 'LDO' }));
    await waitFor(() => expect(b.callsTo('calc.fields').at(-1)).toEqual({ kind: 'ldo' }));
  });
});

describe('FigureView', () => {
  it('shows a figure with its description form', async () => {
    const fig = (recorded('figures.list') as { id: number; caption: string }[])[0];
    render(<FigureView params={{ id: fig.id }} />);
    expect((await screen.findAllByText(new RegExp(fig.caption))).length).toBeGreaterThan(0);
    expect(screen.getAllByRole('img').length).toBeGreaterThan(0);
  });
});

describe('PageView', () => {
  it('shows a page and moves between pages', async () => {
    render(<PageView params={{ ds: 1, page: 0 }} />);
    await waitFor(() => expect(b.callsTo('datasheet.get').length).toBeGreaterThan(0));
    const img = await screen.findAllByRole('img');
    expect(img[0].getAttribute('src')).toContain('/api/page.png');
  });
});

describe('NotesPanel', () => {
  it('lists notes and adds one', async () => {
    render(<NotesPanel kind="part" id="SD1234" datasheetId={1} />);
    expect(await screen.findByText(/for the 3V3 rail/)).toBeInTheDocument();
    const box = screen.getByPlaceholderText(/Add a note/);
    fireEvent.change(box, { target: { value: 'Check the inductor saturation current.' } });
    fireEvent.keyDown(box, { key: 'Enter', ctrlKey: true });
    await waitFor(() => expect(b.callsTo('notes.add')[0]).toMatchObject({ targetKind: 'part', targetId: 'SD1234', body: 'Check the inductor saturation current.' }));
  });
});

describe('SettingsView', () => {
  it('shows settings and saves a change', async () => {
    render(<SettingsView />);
    expect(screen.getByText(/Agent access/)).toBeInTheDocument();
    expect(screen.getByText(/Parts catalogue/)).toBeInTheDocument();
    expect(screen.getByText(/Translation/, { selector: 'h3' })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('switch', { name: /Allow agents/ }));
    await waitFor(() => expect(b.callsTo('settings.update').length).toBe(1));
  });
});

describe('CatalogView', () => {
  it('offers the download when the catalogue is missing', async () => {
    render(<CatalogView params={{}} />);
    expect((await screen.findAllByText(/Download/)).length).toBeGreaterThan(0);
  });
});

describe('Welcome', () => {
  it('opens the import dialog', () => {
    render(<Welcome />);
    fireEvent.click(screen.getByText('Import datasheets'));
    expect(useStore.getState().importOpen).toBe(true);
  });
});

describe('ViewHost', () => {
  it('shows the open tab and keeps others mounted', async () => {
    act(() => {
      useStore.getState().open({ kind: 'project', title: 'Sensor board', params: { project: 'Sensor board' } });
      useStore.getState().open({ kind: 'settings', title: 'Settings', params: {} });
    });
    const { container } = render(<ViewHost />);
    expect(await screen.findByText(/Agent access/)).toBeVisible();
    const views = container.querySelectorAll(':scope > .view');
    expect(views).toHaveLength(2);
    expect(within(views[0] as HTMLElement).queryByText(/Agent access/)).toBeNull();
  });
});
