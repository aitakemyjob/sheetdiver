import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { columns } from '../lib/blockLayout';
import { kindOf, layoutBlockDiagram, overlaps } from '../lib/blockDiagram';
import { useStore } from '../lib/store';
import type { Block, ConnectionRow, DiagramEdge, DiagramNode, Net, ProjectDetail, ProjectPart } from '../lib/types';
import { fakeBackend, recorded, type FakeBackend } from '../test/backend';
import { connectionsCSV } from './ProjectConnections';
import { ProjectView } from './ProjectView';

let b: FakeBackend;
beforeEach(() => {
  b = fakeBackend();
});

const node = (id: string, extra: Partial<DiagramNode> = {}): DiagramNode => ({ id, label: id, ...extra });
const edge = (from: string, to: string, kind: DiagramEdge['kind'] = 'power'): DiagramEdge => ({ from, to, label: `${from}-${to}`, kind, nets: [] });

describe('columns', () => {
  it('follow the power flow, with signal-only parts right of who they talk to', () => {
    // USB feeds the charger, which feeds the regulator, which feeds the MCU and a sensor; the sensor also talks to the MCU.
    const nodes = ['usb', 'chg', 'reg', 'mcu', 'sensor', 'r1'].map((id) => node(id));
    const edges = [edge('usb', 'chg'), edge('chg', 'reg'), edge('reg', 'mcu'), edge('reg', 'sensor'), edge('mcu', 'sensor', 'signal')];
    expect(Object.fromEntries(columns(nodes, edges))).toEqual({ usb: 0, chg: 1, reg: 2, mcu: 3, sensor: 4, r1: 5 });
  });

  it('survive a loop in the power tree', () => {
    const col = columns([node('a'), node('b')], [edge('a', 'b'), edge('b', 'a')]);
    expect([...col.values()].every((c) => c < 2)).toBe(true);
  });
});

/** A project detail with only the given connection table. */
function project(parts: Partial<ProjectPart>[], blocks: Partial<Block>[], rows: [string, string, string?, string?][], nets: Partial<Net>[] = []): ProjectDetail {
  const base = recorded('project.detail', { project: 'Empty board' }) as ProjectDetail;
  const ps = parts.map((p, i) => ({ id: i + 1, mpn: 'X', category: 'buck', qty: 1, hasDatasheet: true, projectId: 1, partId: i + 1, ...p })) as ProjectPart[];
  const bs = blocks.map((b, i) => ({ id: i + 1, source: 'user', ...b })) as Block[];
  const netNames = [...new Set(rows.map((r) => r[0]))];
  const allNets = netNames.map((n, i) => ({ id: i + 1, name: n, kind: /^GND/.test(n) ? 'ground' : 'signal', source: 'user', ...nets.find((x) => x.name === n) })) as Net[];
  const conns: ConnectionRow[] = rows.map(([net, end, pin, pinName], i) => {
    const p = ps.find((x) => x.designator === end);
    const b = bs.find((x) => x.name === end);
    return { id: i + 1, netId: allNets.find((n) => n.name === net)!.id, net, entryId: p?.id, blockId: p ? undefined : b!.id, pin, pinName: pinName ?? pin, source: 'user', endpoint: end };
  });
  return { ...base, parts: ps, connections: { nets: allNets, blocks: bs, connections: conns, diagram: { nodes: [], edges: [] }, pinouts: [] } };
}

describe('block diagram layout', () => {
  // The USB-C to 3V3 sketch: connector, eFuse, buck-boost, with their passives.
  const d = project(
    [
      { designator: 'U1', mpn: 'TPS25200', supplyRail: 'VBUS', outputRail: '5V_PROT' },
      { designator: 'U2', mpn: 'TPS63900', supplyRail: '5V_PROT', outputRail: '3V3' },
    ],
    [
      { name: 'J1 USB-C', kind: 'connector' },
      { name: 'D1', kind: 'diode', value: 'TVS' },
      { name: 'C1', kind: 'capacitor', value: '10µF' },
      { name: 'C2', kind: 'capacitor', value: '10µF' },
      { name: 'L1', kind: 'inductor', value: '2.2µH' },
      { name: 'R4', kind: 'resistor', value: '36.5k' },
    ],
    [
      ['VBUS', 'J1 USB-C', 'VBUS'],
      ['GND', 'J1 USB-C', 'GND'],
      ['VBUS', 'D1'],
      ['VBUS', 'U1', '1', 'IN'],
      ['VBUS', 'U1', '2', 'EN'],
      ['5V_PROT', 'U1', '3', 'OUT'],
      ['5V_PROT', 'C1'],
      ['5V_PROT', 'C2'],
      ['5V_PROT', 'U2', '10', 'VIN'],
      ['GND', 'U2', '2', 'SEL'],
      ['GND', 'U2', '8', 'GND'],
      ['3V3', 'U2', '6', 'VOUT'],
      ['LX1', 'U2', '9', 'LX1'],
      ['LX2', 'U2', '7', 'LX2'],
      ['LX1', 'L1', '1'],
      ['LX2', 'L1', '2'],
      ['CFG1', 'U2', '3', 'CFG1'],
      ['CFG1', 'R4'],
    ],
  );
  const sch = layoutBlockDiagram(d);
  const box = (t: string) => sch.boxes.find((b) => b.title.startsWith(t))!;
  const pin = (t: string, name: string) => box(t).pins.find((p) => p.name === name)!;

  it('draws parts left to right along the power flow', () => {
    expect(sch.boxes.map((b) => b.title)).toEqual(expect.arrayContaining(['J1 USB-C', 'U1 TPS25200', 'U2 TPS63900']));
    expect(box('J1').x).toBeLessThan(box('U1').x);
    expect(box('U1').x).toBeLessThan(box('U2').x);
    expect(sch.boxes).toHaveLength(3); // passives aren't boxes with pins
  });

  it('puts pins on the side of what they connect to', () => {
    expect(pin('U1', 'IN').side).toBe('l');
    expect(pin('U1', 'OUT').side).toBe('r');
    expect(pin('U2', 'VIN').side).toBe('l');
    expect(pin('U2', 'VOUT').side).toBe('r');
    expect(pin('U2', 'CFG1').side).toBe('b');
    expect([pin('U2', 'LX1').side, pin('U2', 'LX2').side]).toEqual(['r', 'r']);
    // Tied pins are written inside; ground is a label.
    expect(pin('U1', 'EN')).toMatchObject({ side: 'in', tied: 'IN' });
    expect(pin('U2', 'SEL')).toMatchObject({ side: 'in', tied: 'GND' });
    expect(box('U2').ground).toEqual(['GND']);
  });

  it('wires nets between parts and hangs the passives', () => {
    const vbus = sch.wires.find((w) => w.label?.text === 'VBUS')!;
    expect(vbus.path.startsWith(`M${pin('J1', 'VBUS').x},`)).toBe(true);
    // The 3V3 output leaves with an arrow.
    expect(sch.wires.find((w) => w.label?.text === '3V3')).toMatchObject({ arrow: true });
    const p = (l: string) => sch.passives.find((x) => x.label === l)!;
    // C1 and C2 are one stub, under the 5V_PROT wire between U1 and U2.
    expect(p('C1,C2')).toMatchObject({ value: '10µF', kind: 'capacitor' });
    expect(p('C1,C2').x).toBeGreaterThan(box('U1').x + box('U1').w);
    expect(p('C1,C2').x + p('C1,C2').w).toBeLessThan(box('U2').x);
    // The TVS hangs from VBUS, R4 below CFG1, L1 across LX1 and LX2.
    expect(p('D1').y).toBeGreaterThan(pin('J1', 'VBUS').y);
    expect(p('R4').y).toBeGreaterThan(box('U2').y + box('U2').h);
    expect(p('L1').x).toBeGreaterThan(box('U2').x + box('U2').w);
    expect(sch.grounded).toBe(true);
  });

  it('keeps a saved position', () => {
    const moved = layoutBlockDiagram({ ...d, connections: { ...d.connections, diagram: { nodes: [{ id: 'part:2', label: 'U2', x: 900, y: 300 }], edges: [] } } });
    expect(moved.boxes.find((b) => b.id === 'part:2')).toMatchObject({ x: 900, y: 300 });
  });

  it('never overlaps: parts, passives and labels', () => {
    const shapes = (x: ReturnType<typeof layoutBlockDiagram>) => [
      ...x.boxes.map((b) => ({ what: b.title, x: b.x, y: b.y - 30, w: b.w, h: b.h + 30 })),
      ...x.passives.map((p) => ({ what: p.label, x: p.x, y: p.y, w: p.w, h: p.h })),
    ];
    const clashes = (x: ReturnType<typeof layoutBlockDiagram>) => {
      const all = shapes(x);
      const out: string[] = [];
      all.forEach((a, i) => all.slice(i + 1).forEach((b) => overlaps(a, b) && out.push(`${a.what} / ${b.what}`)));
      return out;
    };
    expect(clashes(sch)).toEqual([]);
    // A part dropped on top of another is moved off it.
    const u1 = box('U1');
    const dropped = layoutBlockDiagram({ ...d, connections: { ...d.connections, diagram: { nodes: [{ id: 'part:2', label: 'U2', x: u1.x + 20, y: u1.y + 10 }], edges: [] } } });
    expect(clashes(dropped)).toEqual([]);
  });

  it('knows kinds', () => {
    expect(kindOf('tvs', false)).toBe('diode');
    expect(kindOf('buck', false)).toBe('ic');
    expect(kindOf('', true)).toBe('connector');
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
    expect(call.positions[0].node).toBe(sensor().connections.diagram.nodes.find((n) => n.label === 'U2')!.id);
    fireEvent.click(screen.getByRole('button', { name: /Auto layout/ }));
    await waitFor(() => expect(b.callsTo('connections.change')[1]).toMatchObject({ resetLayout: true }));
  });

  it('lists the connection table and connects a pin', async () => {
    b.on('connections.change', () => ({ problems: ['U2 has no pin “99” in its pin table (saved as given)'] }));
    render(<ProjectView params={{ project: 'Sensor board', tab: 'connections' }} />);
    const table = await screen.findByRole('table');
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
    await screen.findByRole('table');
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

  it('exports the table as CSV', () => {
    const csv = connectionsCSV(sensor());
    expect(csv.split('\n')[0]).toBe('Net,Kind,Bus,Part,Pin,Pin name,Pin type,Notes');
    expect(csv).toContain('SDA,signal,I2C1,U2,4,VFB,I,');
  });
});
