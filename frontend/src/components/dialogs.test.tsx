import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { useStore } from '../lib/store';
import { fakeBackend, recorded, type FakeBackend } from '../test/backend';
import type { Datasheet } from '../lib/types';
import { ConnectAgent, connectSnippets } from './ConnectAgent';
import { ImportDialog } from './ImportDialog';
import { ImportQueue } from './ImportQueue';
import { ReportDialog } from './ReportDialog';
import { AllUpdatesDialog, UpdateDialog } from './UpdateDialog';

let b: FakeBackend;
beforeEach(() => {
  b = fakeBackend();
  useStore.setState({ importOpen: true, connectOpen: true, queueOpen: true });
});

/** Switches the import dialog to a mode. */
function mode(name: string) {
  fireEvent.click(screen.getByRole('tab', { name }));
}

describe('ImportDialog', () => {
  it('imports part numbers looked up at the manufacturers', async () => {
    render(<ImportDialog />);
    expect(screen.getByRole('dialog', { name: 'Import datasheets' })).toBeInTheDocument();
    mode('Part numbers');
    fireEvent.change(screen.getByPlaceholderText(/TPS563200/), { target: { value: 'TPS563200\nMAX77801, STM32U575' } });
    fireEvent.click(screen.getByRole('button', { name: /^Import 3/ }));
    await waitFor(() => expect(b.callsTo('imports.start')).toEqual([{ mpns: ['TPS563200', 'MAX77801', 'STM32U575'] }]));
    expect(useStore.getState().importOpen).toBe(false);
  });

  it('imports URLs', async () => {
    render(<ImportDialog />);
    mode('URLs');
    fireEvent.change(screen.getByPlaceholderText(/ti.com/), { target: { value: 'https://example.com/a.pdf' } });
    fireEvent.click(screen.getByRole('button', { name: /^Import/ }));
    await waitFor(() => expect(b.callsTo('imports.start')).toEqual([{ urls: ['https://example.com/a.pdf'] }]));
  });

  it('builds a project from a BOM', async () => {
    b.on('bom.import', () => ({ project: 'BOM', rows: [], counts: { 'in library': 1 } }));
    render(<ImportDialog />);
    mode('BOM');
    fireEvent.change(screen.getByPlaceholderText(/Reference,Value,MPN/), { target: { value: 'Reference,Value,MPN,Qty\nU1,,SD1234,1' } });
    fireEvent.click(screen.getByRole('button', { name: 'Import BOM' }));
    await waitFor(() => expect(b.callsTo('bom.import')[0]).toMatchObject({ text: 'Reference,Value,MPN,Qty\nU1,,SD1234,1', fetch: true }));
  });

  it('imports a folder', async () => {
    render(<ImportDialog />);
    mode('Folder');
    const input = screen.getByPlaceholderText(/Datasheets/);
    fireEvent.change(input, { target: { value: '~/Datasheets' } });
    fireEvent.click(screen.getAllByRole('button', { name: /^Import/ }).at(-1)!);
    await waitFor(() => expect(b.callsTo('imports.start')[0]).toMatchObject({ folder: '~/Datasheets' }));
  });

  it('searches the catalogue when it is downloaded', async () => {
    useStore.setState({ catalog: { ...(recorded('catalog.status') as object), installed: true } as never });
    b.on('catalog.search', () => [{ lcsc: 'C16581', mpn: 'TP4056', manufacturer: 'TOPPOWER', description: 'Charger', package: 'SOP-8', stock: 1000, price: 0.1, basic: true, datasheet: 'x', subcategory: 'Battery Management' }]);
    render(<ImportDialog />);
    mode('Catalogue');
    fireEvent.change(screen.getAllByRole('textbox')[0], { target: { value: 'TP4056' } });
    expect(await screen.findByText('TP4056')).toBeInTheDocument();
  });

  it('closes', () => {
    render(<ImportDialog />);
    fireEvent.click(screen.getByRole('button', { name: /Close/ }));
    expect(useStore.getState().importOpen).toBe(false);
  });
});

describe('ImportQueue', () => {
  it('shows jobs and retries a failed one', async () => {
    useStore.setState({
      jobs: {
        1: { id: 1, name: 'a.pdf', source: 'a.pdf', status: 'done', done: 3, total: 3, started: '', finished: '' },
        2: { id: 2, name: 'b.pdf', source: 'https://x/b.pdf', status: 'failed', error: 'not a PDF', done: 0, total: 0, started: '', finished: '' },
        3: { id: 3, name: 'c.pdf', source: 'c.pdf', status: 'running', stage: 'reading', done: 1, total: 4, started: '', finished: '' },
      },
    });
    render(<ImportQueue />);
    expect(screen.getByText('not a PDF')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /^Retry$/ }));
    await waitFor(() => expect(b.callsTo('imports.retry')).toEqual([{ id: 2 }]));
  });
});

describe('ReportDialog', () => {
  it('shows the design review and saves it', async () => {
    const app = useStore.getState().app!;
    useStore.setState({ app: { ...app, canSaveFile: true } });
    b.on('file.save', () => ({ path: '/home/me/Sensor board review.md' }));
    render(<ReportDialog project="Sensor board" onClose={() => {}} />);
    expect((await screen.findAllByText(/design review/)).length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole('button', { name: /Save/ }));
    await waitFor(() => expect(b.callsTo('file.save')).toHaveLength(1));
    const saved = b.callsTo('file.save')[0] as { name: string; data: string };
    expect(new TextDecoder().decode(Uint8Array.from(atob(saved.data), (c) => c.charCodeAt(0)))).toContain('# Sensor board — design review');
    expect(useStore.getState().toasts.at(-1)?.title).toBe('Report saved');
  });
});

describe('UpdateDialog', () => {
  it('checks for a newer revision', async () => {
    b.on('datasheet.checkUpdate', () => ({ datasheetId: 1, filename: 'sd.pdf', current: 'rev A', latest: 'rev A', upToDate: true }));
    render(<UpdateDialog ds={{ id: 1, filename: 'sd.pdf' } as Datasheet} onClose={() => {}} />);
    await waitFor(() => expect(b.callsTo('datasheet.checkUpdate')).toEqual([{ id: 1 }]));
  });

  it('checks every datasheet', async () => {
    b.on('datasheets.checkUpdates', () => []);
    render(<AllUpdatesDialog onClose={() => {}} />);
    await waitFor(() => expect(b.callsTo('datasheets.checkUpdates').length).toBe(1));
  });
});

describe('ConnectAgent', () => {
  it('shows how to connect an agent to the MCP server', () => {
    render(<ConnectAgent />);
    const dialog = screen.getByRole('dialog');
    expect(within(dialog).getAllByText(/claude mcp add/).length).toBeGreaterThan(0);
    expect(within(dialog).getAllByText(/\/mcp/).length).toBeGreaterThan(0);
  });

  /** Opens the dialog in the desktop app, at the given client. */
  function desktopAt(client: string) {
    const app = useStore.getState().app!;
    useStore.setState({ app: { ...app, canSaveFile: true, platform: 'windows', stdio: { command: 'C:\\Program Files\\SheetDiver\\sheetdiver-mcp.exe', args: [] } } });
    b.on('file.save', (a) => ({ path: `C:\\Users\\me\\${(a as { name: string }).name}` }));
    render(<ConnectAgent />);
    fireEvent.click(screen.getByRole('button', { name: client }));
  }

  it('saves the Claude Desktop extension', async () => {
    b.on('mcp.extension', () => ({ name: 'sheetdiver.mcpb', data: btoa('PK') }));
    desktopAt('Claude Desktop');
    fireEvent.click(screen.getByRole('button', { name: /Save Claude Desktop extension/ }));
    await waitFor(() => expect(b.callsTo('file.save')[0]).toEqual({ name: 'sheetdiver.mcpb', data: btoa('PK') }));
    await waitFor(() => expect(useStore.getState().toasts.at(-1)).toMatchObject({ kind: 'ok', title: 'Claude Desktop extension saved' }));
    expect(screen.getByText(/opens the app when Claude needs it/)).toBeInTheDocument();
    // The hand-written config runs the installed console bridge, not a "sheetdiver" on PATH.
    expect(screen.getByText(/"command": "C:\\\\Program Files\\\\SheetDiver\\\\sheetdiver-mcp.exe",\s+"args": \[\]/)).toBeInTheDocument();
  });

  it("saves a client's config file", async () => {
    desktopAt('Codex');
    expect(screen.getByText('codex mcp add sheetdiver -- "C:\\Program Files\\SheetDiver\\sheetdiver-mcp.exe"')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /Save as config.toml/ }));
    await waitFor(() => expect(b.callsTo('file.save').length).toBe(1));
    const { name, data } = b.callsTo('file.save')[0] as { name: string; data: string };
    expect(name).toBe('config.toml');
    expect(atob(data)).toContain("command = 'C:\\Program Files\\SheetDiver\\sheetdiver-mcp.exe'\nargs = []");
    expect(screen.getByText(/paste the snippet into it instead/)).toBeInTheDocument();
  });

  it('reports a failed save', async () => {
    b.on('mcp.extension', () => {
      throw new Error('disk full');
    });
    desktopAt('Claude Desktop');
    fireEvent.click(screen.getByRole('button', { name: /Save Claude Desktop extension/ }));
    await waitFor(() => expect(useStore.getState().toasts.at(-1)).toMatchObject({ kind: 'err', body: 'disk full' }));
  });
});

describe('connectSnippets', () => {
  it('writes each client its own format', () => {
    const sn = connectSnippets('http://127.0.0.1:47480/mcp', { command: '/Applications/SheetDiver.app/Contents/MacOS/SheetDiver', args: ['mcp'] }, 'darwin');
    const parse = (c: keyof typeof sn, i = 0) => JSON.parse(sn[c][i].text);
    expect(parse('Claude Desktop').mcpServers.sheetdiver).toEqual({ command: '/Applications/SheetDiver.app/Contents/MacOS/SheetDiver', args: ['mcp'] });
    expect(sn['Claude Desktop'][0].note).toContain('~/Library/Application Support/Claude/');
    expect(parse('Claude Code', 1).mcpServers.sheetdiver).toEqual({ type: 'http', url: 'http://127.0.0.1:47480/mcp' });
    expect(parse('Cursor').mcpServers.sheetdiver).toEqual({ url: 'http://127.0.0.1:47480/mcp' });
    expect(parse('VS Code').servers.sheetdiver).toEqual({ type: 'http', url: 'http://127.0.0.1:47480/mcp' });
    expect(parse('Gemini CLI', 1).mcpServers.sheetdiver).toEqual({ httpUrl: 'http://127.0.0.1:47480/mcp' });
    expect(parse('Windsurf').mcpServers.sheetdiver).toEqual({ serverUrl: 'http://127.0.0.1:47480/mcp' });
    expect(sn.Other[1].text).toBe('/Applications/SheetDiver.app/Contents/MacOS/SheetDiver mcp');
    expect(sn.Codex[1].text).toContain('args = ["mcp"]');
  });
});
