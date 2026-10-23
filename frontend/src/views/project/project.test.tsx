import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { useStore } from '../../lib/store';
import type { ProjectDetail } from '../../lib/types';
import { fakeBackend, recorded, type FakeBackend } from '../../test/backend';
import { ProjectView } from '../ProjectView';
import { darkTheme, palette, swatch } from '../../lib/diagramPalette';
import { fitView, zoomAbout } from './BlockDiagram';
import { connectionsCSV } from './ConnectionTable';
import { designators } from './connections';
import { pinoutsCSV } from './PinTables';

let b: FakeBackend;
beforeEach(() => {
  b = fakeBackend();
});

describe('the view', () => {
  it('zooms about a point, keeping it in place', () => {
    const v = zoomAbout({ k: 1, x: 0, y: 0 }, 2, 100, 50);
    expect(v).toEqual({ k: 2, x: -100, y: -50 });
    // The drawing point under (100, 50) stays there.
    expect((100 - v.x) / v.k).toBe(100);
    expect(zoomAbout({ k: 4, x: 0, y: 0 }, 2, 0, 0).k).toBe(4); // at most 400%
  });

  it('fits a drawing, never above 100%', () => {
    expect(fitView(2000, 500, 1000, 500)).toEqual({ k: 0.5, x: 0, y: 125 });
    expect(fitView(200, 100, 1000, 500).k).toBe(1);
    expect(fitView(200, 100, 0, 0)).toEqual({ k: 1, x: 0, y: 0 }); // not laid out yet
  });
});

describe('project block diagram, pins and connections', () => {
  const sensor = () => recorded('project.detail', { project: 'Sensor board' }) as ProjectDetail;

  it('draws the block diagram from the project', async () => {
    render(<ProjectView params={{ project: 'Sensor board', tab: 'diagram', highlight: 'SDA' }} />);
    const svg = await screen.findByRole('img', { name: 'Block diagram of Sensor board' });
    for (const t of ['U1 TPX5430', 'U2 SD1235', 'Battery']) expect(svg.querySelector(`[data-part="${t}"]`)).not.toBeNull();
    // U2's VFB pin is wired to SDA; the agent's highlight marks that net.
    expect(within(svg as unknown as HTMLElement).getByText('VFB')).toBeInTheDocument();
    expect(svg.querySelector('[data-net="SDA"]')).toHaveAttribute('data-hl', 'true');
    expect(svg.querySelector('[data-net="VIN"]')).not.toHaveAttribute('data-hl');
  });

  it('saves where a block is dragged', async () => {
    render(<ProjectView params={{ project: 'Sensor board', tab: 'diagram' }} />);
    const svg = await screen.findByRole('img', { name: /Block diagram/ });
    const u2 = svg.querySelector('[data-part="U2 SD1235"]')!;
    fireEvent.pointerDown(u2, { clientX: 10, clientY: 10, pointerId: 1 });
    fireEvent.pointerMove(svg, { clientX: 60, clientY: 90, pointerId: 1 });
    fireEvent.pointerUp(svg, { pointerId: 1 });
    await waitFor(() => expect(b.callsTo('connections.change').length).toBe(1));
    const call = b.callsTo('connections.change')[0] as { project: string; positions: { node: string; x: number; y: number }[] };
    expect(call.project).toBe('Sensor board');
    expect(call.positions[0].node).toBe(sensor().connections.diagram.parts.find((p) => p.title === 'U2 SD1235')!.id);
    fireEvent.click(screen.getByRole('button', { name: /Auto layout/ }));
    await waitFor(() => expect(b.callsTo('connections.change')[1]).toMatchObject({ resetLayout: true }));
  });

  it('lists the connection table and connects a pin', async () => {
    b.on('connections.change', () => ({ problems: ['U2 has no pin “99” in its pin table (saved as given)'] }));
    render(<ProjectView params={{ project: 'Sensor board', tab: 'connections' }} />);
    const table = await screen.findByRole('table', { name: 'Connection table' });
    // Each net with the pins on it, names from the pin table.
    const sda = within(table).getByText('SDA').closest('tr')!;
    expect(sda).toBeInTheDocument();
    expect(within(table).getAllByText('VFB').length).toBeGreaterThan(0);
    fireEvent.change(screen.getByLabelText('Net'), { target: { value: 'SCL' } });
    fireEvent.change(screen.getByLabelText('Part or block'), { target: { value: 'U2' } });
    fireEvent.change(screen.getByLabelText('Pin'), { target: { value: '99' } });
    fireEvent.click(screen.getByRole('button', { name: /^Connect$/ }));
    await waitFor(() => expect(b.callsTo('connections.change')[0]).toEqual({ project: 'Sensor board', connect: [{ net: 'SCL', part: 'U2', pin: '99' }] }));
    // What couldn't be applied is shown.
    await waitFor(() => expect(useStore.getState().toasts.at(-1)).toMatchObject({ kind: 'warn', body: expect.stringContaining('no pin “99”') }));
  });

  it('edits nets and blocks', async () => {
    render(<ProjectView params={{ project: 'Sensor board', tab: 'connections' }} />);
    await screen.findByRole('table', { name: 'Connection table' });
    fireEvent.change(screen.getByLabelText('SDA kind'), { target: { value: 'power' } });
    await waitFor(() => expect(b.callsTo('connections.change')[0]).toMatchObject({ nets: [{ name: 'SDA', kind: 'power' }] }));
    fireEvent.change(screen.getByLabelText('New block'), { target: { value: 'USB-C' } });
    fireEvent.click(screen.getByRole('button', { name: /^Block$/ }));
    await waitFor(() => expect(b.callsTo('connections.change')[1]).toMatchObject({ blocks: [{ name: 'USB-C' }] }));
    fireEvent.click(screen.getByRole('button', { name: 'Remove Battery' }));
    await waitFor(() => expect(b.callsTo('connections.change')[2]).toMatchObject({ blocks: [{ name: 'Battery', remove: true }] }));
  });

  it('zooms and fits the block diagram', async () => {
    render(<ProjectView params={{ project: 'Sensor board', tab: 'diagram' }} />);
    await screen.findByRole('img', { name: /Block diagram/ });
    const level = () => screen.getByRole('button', { name: 'Fit' }).textContent;
    const before = level();
    fireEvent.click(screen.getByRole('button', { name: 'Zoom in' }));
    expect(level()).not.toBe(before);
    fireEvent.click(screen.getByRole('button', { name: 'Zoom out' }));
    expect(level()).toBe(before);
  });

  it("lists each part's pins with what they connect to", async () => {
    render(<ProjectView params={{ project: 'Sensor board', tab: 'pins', highlight: 'SDA' }} />);
    expect(await screen.findByText('U2 SD1235')).toBeInTheDocument();
    const card = screen.getByText('U2 SD1235').closest('.card')! as HTMLElement;
    // Every pin of its pin table, connected or not.
    const vfb = within(card).getByText('VFB').closest('tr')!;
    expect(vfb).toHaveTextContent('SDA');
    expect(vfb).toHaveTextContent('U1');
    expect(vfb).toHaveClass('agent-hl');
    expect(within(card).getByText('SW').closest('tr')).toHaveTextContent('not connected');
    expect(within(card).getByText(/of 4 pins connected/)).toBeInTheDocument();
  });

  it('exports the pin tables as CSV', () => {
    const csv = pinoutsCSV(sensor());
    expect(csv.split('\n')[0]).toBe('Part,Pin,Name,Type,Net,Connected to,Description');
    expect(csv).toContain('U2 SD1235,4,VFB,I,SDA,U1,Feedback input.');
  });

  it('shows the circuits drawn by the backend', async () => {
    render(<ProjectView params={{ project: 'USB-C to 3V3', tab: 'diagram', highlight: 'L1' }} />);
    const svg = await screen.findByRole('img', { name: 'Block diagram of USB-C to 3V3' });
    expect(svg.querySelector('[data-passive="C1,C2"]')).not.toBeNull();
    expect(svg.querySelector('[data-passive="L1"]')).toHaveAttribute('data-hl', 'true');
    // EN is drawn as a pin of each IC, tied to IN / VIN at a junction.
    expect(within(svg as unknown as HTMLElement).getAllByText('EN')).toHaveLength(2);
    expect(svg.querySelectorAll('[data-junction]').length).toBeGreaterThan(0);
    expect(svg.querySelectorAll('[data-ground]').length).toBeGreaterThan(0);
  });

  it('dims the unconnected pins of a generated symbol, and hides them on request, remembering that', async () => {
    localStorage.removeItem('sheetdiver.diagram.connectedOnly');
    render(<ProjectView params={{ project: 'MCU board', tab: 'diagram' }} />);
    const svg = await screen.findByRole('img', { name: 'Block diagram of MCU board' });
    // U1 SD1234's pin table has VFB, which nothing is wired to.
    await waitFor(() => expect(svg.querySelector('[data-part="U1 SD1234"] [data-pin="VFB"]')).toHaveAttribute('data-dim', 'true'));
    expect(svg.querySelector('[data-part="U1 SD1234"] [data-pin="VIN"]')).not.toHaveAttribute('data-dim');
    const toggle = screen.getByRole('button', { name: /All pins/ });
    expect(toggle).toHaveAttribute('aria-pressed', 'false');
    fireEvent.click(toggle);
    await waitFor(() => expect(svg.querySelector('[data-dim]')).toBeNull());
    expect(svg.querySelector('[data-part="U1 SD1234"] [data-pin="VIN"]')).not.toBeNull();
    expect(screen.getByRole('button', { name: /Connected pins only/ })).toHaveAttribute('aria-pressed', 'true');
    expect(localStorage.getItem('sheetdiver.diagram.connectedOnly')).toBe('1');
    // The choice holds for the next board.
    cleanup();
    render(<ProjectView params={{ project: 'MCU board', tab: 'diagram' }} />);
    expect(await screen.findByRole('button', { name: /Connected pins only/ })).toHaveAttribute('aria-pressed', 'true');
    localStorage.removeItem('sheetdiver.diagram.connectedOnly');
  });

  it('draws a block of a transistor kind as a transistor, and a two-terminal with its symbol', async () => {
    render(<ProjectView params={{ project: 'Driver board', tab: 'diagram' }} />);
    const svg = await screen.findByRole('img', { name: 'Block diagram of Driver board' });
    expect(svg.querySelector('[data-part="Q1"] [data-shape="mosfet-n"]')).not.toBeNull();
    expect(svg.querySelector('[data-part="Q2"] [data-shape="npn"]')).not.toBeNull();
    expect(svg.querySelector('[data-passive="F1"] [data-shape="fuse"]')).not.toBeNull();
    // D2's cathode is on the signal, so it's drawn the other way up.
    expect(svg.querySelector('[data-passive="D2"] [data-shape="led"]')).toHaveAttribute('data-flipped', 'true');
    expect(svg.querySelector('[data-passive="D1"] [data-shape="led"]')).not.toHaveAttribute('data-flipped');
    // U1 has a pin table, so the toggle is offered.
    expect(screen.getByRole('button', { name: /All pins/ })).toBeInTheDocument();
  });

  it('exports the table as CSV', () => {
    const csv = connectionsCSV(sensor());
    expect(csv.split('\n')[0]).toBe('Net,Kind,Bus,Part,Pin,Pin name,Pin type,Notes');
    expect(csv).toContain('SDA,signal,I2C1,U2,4,VFB,I,');
  });

  it('pans, zooms with the wheel, opens a part and exports the drawing', async () => {
    const app = useStore.getState().app!;
    useStore.setState({ app: { ...app, canSaveFile: true } });
    b.on('file.save', (a) => ({ path: `/home/me/${(a as { name: string }).name}` }));
    render(<ProjectView params={{ project: 'USB-C to 3V3', tab: 'diagram' }} />);
    const svg = await screen.findByRole('img', { name: /Block diagram/ });
    const view = svg.querySelector('g[data-view]')!;
    const before = view.getAttribute('transform');
    // Drag the background: the view moves.
    fireEvent.pointerDown(svg, { clientX: 100, clientY: 100, pointerId: 1 });
    fireEvent.pointerMove(svg, { clientX: 160, clientY: 120, pointerId: 1 });
    fireEvent.pointerUp(svg, { pointerId: 1 });
    expect(view.getAttribute('transform')).not.toBe(before);
    expect(b.callsTo('connections.change')).toHaveLength(0); // panning saves nothing
    // The wheel zooms.
    const level = screen.getByRole('button', { name: 'Fit' }).textContent;
    fireEvent.wheel(svg, { deltaY: -200, clientX: 50, clientY: 50 });
    expect(screen.getByRole('button', { name: 'Fit' }).textContent).not.toBe(level);
    // Double-clicking a part from the library opens it; a block doesn't.
    fireEvent.doubleClick(svg.querySelector('[data-part="J1 USB-C"]')!);
    expect(useStore.getState().tabs.some((t) => t.kind === 'part')).toBe(false);
    // The SVG export is the whole drawing, without the view's zoom.
    fireEvent.click(screen.getByRole('button', { name: /SVG/ }));
    await waitFor(() => expect(b.callsTo('file.save')).toHaveLength(1));
    const saved = b.callsTo('file.save')[0] as { name: string; data: string };
    expect(saved.name).toBe('USB-C to 3V3-block-diagram.svg');
    const text = new TextDecoder().decode(Uint8Array.from(atob(saved.data), (c) => c.charCodeAt(0)));
    expect(text).toMatch(/^<\?xml/);
    expect(text).toContain('viewBox="0 0 ');
    expect(text).not.toMatch(/data-view="[^"]*" transform=/);
    expect(text).not.toContain('var(--');
  });

  it('opens a library part from the diagram', async () => {
    render(<ProjectView params={{ project: 'MCU board', tab: 'diagram' }} />);
    const svg = await screen.findByRole('img', { name: /Block diagram/ });
    fireEvent.doubleClick(svg.querySelector('[data-part="U1 SD1234"]')!);
    expect(useStore.getState().tabs.at(-1)).toMatchObject({ kind: 'part', params: { part: 'SD1234' } });
  });

  it('says when nothing is connected yet', async () => {
    render(<ProjectView params={{ project: 'Empty board', tab: 'diagram' }} />);
    expect(await screen.findByText('Nothing connected yet')).toBeInTheDocument();
  });

  it('exports the pin tables, and says when there are none', async () => {
    render(<ProjectView params={{ project: 'Sensor board', tab: 'pins' }} />);
    await screen.findByText('U2 SD1235');
    fireEvent.click(screen.getByRole('button', { name: /CSV/ }));
    await waitFor(() => expect(useStore.getState().toasts.at(-1)).toMatchObject({ kind: 'ok', title: 'Pin tables saved' }));
    // A part's name opens its pin table.
    fireEvent.click(screen.getByRole('button', { name: 'U2 SD1235' }));
    expect(useStore.getState().tabs.at(-1)).toMatchObject({ kind: 'part', params: { part: 'SD1235', tab: 'pins' } });
  });

  it('says when no part has pins', async () => {
    render(<ProjectView params={{ project: 'Empty board', tab: 'pins' }} />);
    expect(await screen.findByText('No pins yet')).toBeInTheDocument();
  });

  it('puts parts in groups and says what they support', async () => {
    b.on('connections.change', () => ({ problems: [] }));
    render(<ProjectView params={{ project: 'Grouped MCU board', tab: 'connections' }} />);
    const table = await screen.findByRole('table', { name: 'Groups and support' });
    // The groups, with their description and notes.
    expect(screen.getByLabelText("Power's description")).toHaveValue('USB 5 V to 3.3 V');
    expect(screen.getByLabelText("Power's notes")).toHaveValue("Keep the buck's input loop short.");
    // Each part's group and what it supports: set, or guessed.
    expect(within(table).getByLabelText("U1's group")).toHaveValue('Power');
    expect(within(table).getByLabelText('What C4 supports')).toHaveValue('U2 ESP32-C3');
    const c3 = within(table).getByLabelText('What C3 supports') as HTMLSelectElement;
    expect(c3.value).toBe('');
    expect(c3.options[0].text).toBe('auto (U2)');
    // Unset, a passive is drawn in the group of the part it supports.
    expect((within(table).getByLabelText("C4's group") as HTMLSelectElement).options[0].text).toBe('auto (MCU)');

    fireEvent.change(within(table).getByLabelText("C3's group"), { target: { value: 'Sensor' } });
    await waitFor(() => expect(b.callsTo('connections.change')[0]).toEqual({ project: 'Grouped MCU board', assign: [{ part: 'C3', group: 'Sensor' }] }));
    fireEvent.change(within(table).getByLabelText('What C4 supports'), { target: { value: '' } });
    await waitFor(() => expect(b.callsTo('connections.change')[1]).toMatchObject({ assign: [{ part: 'C4', supports: '' }] }));
    // Clearing says it worked, and what happens now.
    await waitFor(() => expect(useStore.getState().toasts.at(-1)).toMatchObject({ kind: 'ok', title: "C4 no longer has a set support: it's guessed from its nets" }));
    const notes = screen.getByLabelText("MCU's notes");
    fireEvent.change(notes, { target: { value: 'Antenna clear of copper' } });
    fireEvent.blur(notes);
    await waitFor(() => expect(b.callsTo('connections.change')[2]).toMatchObject({ groups: [{ name: 'MCU', notes: 'Antenna clear of copper' }] }));
    fireEvent.change(screen.getByLabelText('New group'), { target: { value: 'Debug' } });
    fireEvent.click(screen.getByRole('button', { name: /^Group$/ }));
    await waitFor(() => expect(b.callsTo('connections.change')[3]).toMatchObject({ groups: [{ name: 'Debug' }] }));
    fireEvent.click(screen.getByRole('button', { name: /Remove Sensor/ }));
    await waitFor(() => expect(b.callsTo('connections.change')[4]).toMatchObject({ groups: [{ name: 'Sensor', remove: true }] }));
  });

  it('frames each group on the block diagram', async () => {
    render(<ProjectView params={{ project: 'Grouped MCU board', tab: 'diagram' }} />);
    await screen.findByRole('img', { name: /Block diagram/ });
    const frames = document.querySelectorAll('[data-group]');
    expect([...frames].map((f) => f.getAttribute('data-group'))).toEqual(['Power', 'MCU', 'Sensor']);
    expect(screen.getByText('Notes: Keep the buck\'s input loop short.')).toBeInTheDocument();
  });

  it('edits rows, buses and nets in the connection table', async () => {
    render(<ProjectView params={{ project: 'Sensor board', tab: 'connections' }} />);
    await screen.findByRole('table', { name: 'Connection table' });
    fireEvent.click(screen.getAllByRole('button', { name: 'Disconnect' })[0]);
    await waitFor(() => expect(b.callsTo('connections.change')[0]).toMatchObject({ connect: [{ remove: true }] }));
    const bus = screen.getByLabelText('SDA bus');
    fireEvent.change(bus, { target: { value: 'I2C2' } });
    fireEvent.blur(bus);
    await waitFor(() => expect(b.callsTo('connections.change')[1]).toMatchObject({ nets: [{ name: 'SDA', bus: 'I2C2' }] }));
    fireEvent.click(screen.getByRole('button', { name: 'Delete SDA and its connections' }));
    await waitFor(() => expect(b.callsTo('connections.change')[2]).toMatchObject({ nets: [{ name: 'SDA', remove: true }] }));
    // A passive block with its kind and value.
    fireEvent.change(screen.getByLabelText('New block'), { target: { value: 'R9' } });
    expect([...screen.getByLabelText<HTMLSelectElement>('Block kind').options].map((o) => o.value)).toContain('mosfet-n');
    fireEvent.change(screen.getByLabelText('Block kind'), { target: { value: 'resistor' } });
    fireEvent.change(screen.getByLabelText('Block value'), { target: { value: '10k' } });
    fireEvent.click(screen.getByRole('button', { name: /^Block$/ }));
    await waitFor(() => expect(b.callsTo('connections.change')[3]).toMatchObject({ blocks: [{ name: 'R9', kind: 'resistor', value: '10k' }] }));
    fireEvent.click(screen.getByRole('button', { name: /CSV/ }));
    await waitFor(() => expect(useStore.getState().toasts.at(-1)).toMatchObject({ title: 'Connections saved' }));
  });

  it('reports a change that failed', async () => {
    b.on('connections.change', () => {
      throw new Error('database is locked');
    });
    render(<ProjectView params={{ project: 'Sensor board', tab: 'diagram' }} />);
    await screen.findByRole('img', { name: /Block diagram/ });
    fireEvent.click(screen.getByRole('button', { name: /Auto layout/ }));
    await waitFor(() => expect(useStore.getState().toasts.at(-1)).toMatchObject({ kind: 'err', body: 'database is locked' }));
  });
});

describe('the palette', () => {
  it('follows the theme, and gives unknown kinds a connector\'s colours', () => {
    document.documentElement.style.setProperty('--bg', '#0b1120');
    expect(darkTheme()).toBe(true);
    expect(swatch(palette(), 'ic').fill).toBe('#26215C');
    document.documentElement.style.setProperty('--bg', '#ffffff');
    expect(darkTheme()).toBe(false);
    expect(swatch(palette(), 'relay')).toEqual(palette().connector);
    // The new kinds colour by family: diodes red, transistors blue, odd two-terminals grey.
    expect(swatch(palette(), 'led')).toEqual(palette().diode);
    expect(swatch(palette(), 'mosfet-n')).toEqual(palette().transistor);
    expect(swatch(palette(), 'crystal')).toEqual(palette().other);
    document.documentElement.style.removeProperty('--bg');
    expect(darkTheme()).toBe(false);
  });
});


describe('designators', () => {
  it('offers each of an entry with several', () => {
    expect(designators('R1,R2')).toEqual(['R1,R2', 'R1', 'R2']);
    expect(designators('U1 U2')).toEqual(['U1 U2', 'U1', 'U2']);
    expect(designators('U3')).toEqual(['U3']);
  });
});
