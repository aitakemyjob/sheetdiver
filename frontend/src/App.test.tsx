import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { App } from './App';
import { useStore } from './lib/store';
import { fakeBackend, type FakeBackend } from './test/backend';

let b: FakeBackend;
beforeEach(() => {
  localStorage.clear();
  b = fakeBackend();
  useStore.setState({ ready: false, active: undefined, sidebar: 'library', sidebarOpen: true, importOpen: false });
});

/** Renders the whole app and waits for it to start. */
async function app() {
  const r = render(<App />);
  await screen.findByText('SD1234');
  return r;
}

/** A button in the activity bar by its title. */
function activity(title: string) {
  return screen.getByRole('button', { name: title });
}

describe('App', () => {
  it('starts: loads the state, lists the library, listens for events', async () => {
    await app();
    for (const m of ['app.state', 'activity.list', 'imports.list', 'catalog.status', 'translate.status']) expect(b.callsTo(m).length).toBe(1);
    for (const mpn of ['SD1234', 'SD1235', 'TPX5430']) expect(screen.getByText(mpn)).toBeInTheDocument();
    expect(screen.getByText(/Dive into your datasheets/)).toBeInTheDocument();
  });

  it('opens a part in a tab and closes it', async () => {
    await app();
    fireEvent.click(screen.getByText('SD1234'));
    expect(await screen.findByRole('heading', { name: /SD1234/ })).toBeInTheDocument();
    const tabs = document.querySelector('.tabs') as HTMLElement;
    expect(within(tabs).getByText('SD1234')).toBeInTheDocument();
    act(() => {
      fireEvent.keyDown(window, { key: 'w', ctrlKey: true });
    });
    await waitFor(() => expect(screen.getByText(/Dive into your datasheets/)).toBeInTheDocument());
  });

  it('filters the library', async () => {
    await app();
    fireEvent.change(screen.getByPlaceholderText(/Filter by part number/), { target: { value: 'TPX' } });
    await waitFor(() => expect(b.callsTo('parts.list').at(-1)).toMatchObject({ query: 'TPX' }));
  });

  it('switches sidebar panels and creates a project', async () => {
    await app();
    fireEvent.click(activity('Projects'));
    expect((await screen.findAllByText('Sensor board')).length).toBeGreaterThan(0);
    b.on('projects.create', (a) => ({ id: 3, name: (a as { name: string }).name, description: '', parts: 0, rails: 0, createdAt: '', updatedAt: '' }));
    fireEvent.change(screen.getByPlaceholderText('New project name'), { target: { value: 'Charger board' } });
    fireEvent.keyDown(screen.getByPlaceholderText('New project name'), { key: 'Enter' });
    await waitFor(() => expect(b.callsTo('projects.create')).toEqual([{ name: 'Charger board', description: '' }]));
    fireEvent.click(screen.getAllByText('Sensor board')[0]);
    expect(await screen.findByRole('heading', { name: /^Sensor board/ })).toBeInTheDocument();
    fireEvent.click(activity('Component types'));
    expect((await screen.findAllByText(/Buck/)).length).toBeGreaterThan(0);
  });

  it('opens search, settings and the import dialog from the keyboard and title bar', async () => {
    await app();
    act(() => {
      fireEvent.keyDown(window, { key: 'k', ctrlKey: true });
    });
    expect(await screen.findByPlaceholderText(/efficiency, VFB/)).toBeInTheDocument();
    fireEvent.click(activity('Settings'));
    expect(await screen.findByText(/Agent access/)).toBeInTheDocument();
    act(() => {
      fireEvent.keyDown(window, { key: 'o', ctrlKey: true });
    });
    expect(await screen.findByRole('dialog', { name: /Import datasheets/ })).toBeInTheDocument();
  });

  it('shows notifications from backend events', async () => {
    await app();
    act(() => b.emit({ type: 'import', time: '', data: { id: 9, name: 'x.pdf', source: '', status: 'running', done: 0, total: 3, started: '', finished: '' } }));
    expect((await screen.findAllByText(/x\.pdf|Importing/)).length).toBeGreaterThan(0);
    act(() => useStore.getState().toast({ kind: 'ok', title: 'Hello from a test' }));
    expect(await screen.findByText('Hello from a test')).toBeInTheDocument();
  });

  it('says when the backend is unreachable', async () => {
    b.on('app.state', () => {
      throw new Error('connection refused');
    });
    render(<App />);
    expect(await screen.findByText(/could not reach its backend/)).toBeInTheDocument();
  });
});

describe('App shell', () => {
  it('opens the catalogue, calculators and agent activity', async () => {
    await app();
    fireEvent.click(screen.getByRole('button', { name: /Parts catalogue/ }));
    expect((await screen.findAllByText(/Download/)).length).toBeGreaterThan(0);
    fireEvent.click(activity('Calculators'));
    await waitFor(() => expect(b.callsTo('calc.fields').length).toBeGreaterThan(0));
    fireEvent.click(activity('Agent activity'));
    expect((await screen.findAllByText(/agent|MCP/i)).length).toBeGreaterThan(0);
    act(() => {
      fireEvent.keyDown(window, { key: 'b', ctrlKey: true });
    });
    expect(useStore.getState().sidebarOpen).toBe(false);
  });
});
