import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { App } from '../App';
import { encodeAgentLinks, parseAgentLink } from '../lib/agentLinks';
import { useStore } from '../lib/store';
import { focusToTab } from '../lib/tabs';
import type { CompareColumn, CompareRow, Focus } from '../lib/types';
import { fakeBackend, recorded, type FakeBackend } from '../test/backend';
import { CatalogView } from './CatalogView';
import { CompareView, marks } from './CompareView';
import { ProjectView } from './ProjectView';

let b: FakeBackend;
beforeEach(() => {
  localStorage.clear();
  b = fakeBackend();
  useStore.setState({ walkthrough: undefined, active: undefined });
});

/** Sends the UI what the agent's show tool sends. */
function agentShows(f: Focus) {
  act(() => b.emit({ type: 'focus', time: '', data: f }));
}

describe('focusToTab', () => {
  it('opens comparisons and catalogue candidates', () => {
    expect(focusToTab({ kind: 'compare', parts: ['SD1234', 'TPX5430'], notes: { SD1234: 'cheaper' }, highlight: 'SD1234' })).toMatchObject({
      kind: 'compare',
      title: 'SD1234 vs TPX5430',
      params: { parts: ['SD1234', 'TPX5430'], notes: { SD1234: 'cheaper' }, highlight: 'SD1234' },
    });
    expect(focusToTab({ kind: 'catalog', lcsc: ['C1', 'C2'], highlight: 'C2' })).toMatchObject({ kind: 'catalog', params: { lcsc: ['C1', 'C2'], highlight: 'C2' } });
    expect(focusToTab({ kind: 'compare', parts: [] })).toBeUndefined();
  });

  it('always sets the explanation, so an older one is cleared', () => {
    expect(focusToTab({ kind: 'part', part: 'SD1234', explain: 'Why' })?.params).toMatchObject({ explain: 'Why' });
    const t = focusToTab({ kind: 'part', part: 'SD1234' })!;
    expect('explain' in t.params && t.params.explain === undefined).toBe(true);
  });
});

describe('agent links', () => {
  it('parse', () => {
    expect(parseAgentLink('#page/TPS63900/3')).toEqual({ kind: 'page', target: 'TPS63900', arg: '3' });
    expect(parseAgentLink('#section/SD1234/Electrical%20Characteristics')).toEqual({ kind: 'section', target: 'SD1234', arg: 'Electrical Characteristics' });
    expect(parseAgentLink('#project/Sensor board')).toEqual({ kind: 'project', target: 'Sensor board', arg: '' });
    expect(parseAgentLink('https://ti.com')).toBeUndefined();
    expect(parseAgentLink('#nope/x')).toBeUndefined();
  });

  it('may name targets with spaces', () => {
    expect(encodeAgentLinks('See [Electrical](#section/TPS63900/Electrical Characteristics) and [web](https://a.b/c d).')).toBe(
      'See [Electrical](#section/TPS63900/Electrical%20Characteristics) and [web](https://a.b/c d).',
    );
  });
});

describe('marks', () => {
  const row = (mpn: string, v: { min?: number; typ?: number; max?: number }): CompareRow => ({ part: { mpn } as CompareRow['part'], values: { k: { unit: 'A', page: 0, paramId: 1, source: 'auto', ...v } } });
  const col = (direction: CompareColumn['direction']): CompareColumn => ({ key: 'k', label: 'K', unit: 'A', have: 2, direction });
  it("marks best and worst in the row's direction", () => {
    expect(marks([row('A', { min: 0, max: 3 }), row('B', { min: 0, max: 2 })], col('higher'))).toEqual({ A: 'best', B: 'worst' });
    expect(marks([row('A', { typ: 5e-6 }), row('B', { typ: 1e-4 }), row('C', { typ: 3e-5 })], col('lower'))).toEqual({ A: 'best', B: 'worst' });
  });
  it('leaves rows alone when there is no order or no difference', () => {
    expect(marks([row('A', { typ: 1 }), row('B', { typ: 2 })], col('close'))).toEqual({});
    expect(marks([row('A', { typ: 1 }), row('B', { typ: 1 })], col('lower'))).toEqual({});
    expect(marks([row('A', { typ: 1 }), row('B', {})], col('lower'))).toEqual({});
  });
});

describe('CompareView', () => {
  it('shows the parts side by side with marks, notes and the recommendation', async () => {
    render(<CompareView params={{ parts: ['SD1234', 'TPX5430'], notes: { SD1234: 'Fixed 650 kHz', TPX5430: 'Wider input' }, highlight: 'SD1234' }} />);
    const table = await screen.findByRole('table');
    expect(b.callsTo('compare')[0]).toMatchObject({ parts: ['SD1234', 'TPX5430'] });
    expect(within(table).getByText('recommended').closest('th')).toHaveTextContent('SD1234');
    // Output current: 3 A beats 2 A.
    const iout = within(table).getByText(/Output \/ load current/).closest('tr')!;
    expect(within(iout).getByText('best').closest('td')).toHaveTextContent('3 A');
    expect(within(iout).getByText('worst').closest('td')).toHaveTextContent('2 A');
    expect(within(table).getByText('Wider input')).toBeInTheDocument();
    // A value opens its page.
    fireEvent.click(within(iout).getAllByRole('button')[0]);
    expect(useStore.getState().tabs.at(-1)).toMatchObject({ kind: 'page' });
  });
});

describe('CatalogView candidates', () => {
  it("shows the agent's candidates with its pick, then a normal search", async () => {
    useStore.setState({ catalog: { ...(recorded('catalog.status') as object), installed: true } as never });
    b.on('catalog.get', () => [
      { lcsc: 'C1', mpn: 'TPS63900', manufacturer: 'TI', description: 'Buck-boost', package: 'WSON-10', stock: 500, price: 1.2, library: 'extended', datasheet: '', subcategory: 'DC-DC Converters', attributes: {} },
      { lcsc: 'C2', mpn: 'TPS63802', manufacturer: 'TI', description: 'Buck-boost', package: 'VSON-10', stock: 9000, price: 0.9, library: 'extended', datasheet: '', subcategory: 'DC-DC Converters', attributes: {} },
    ]);
    render(<CatalogView params={{ lcsc: ['C1', 'C2'], highlight: 'C2', nonce: 1 }} />);
    expect(await screen.findByText("Your agent's candidates")).toBeInTheDocument();
    expect(b.callsTo('catalog.get')[0]).toEqual({ lcsc: ['C1', 'C2'] });
    const pick = (await screen.findByText('TPS63802')).closest('tr')!;
    expect(pick).toHaveClass('agent-hl');
    fireEvent.click(screen.getByRole('button', { name: 'Search the catalogue instead' }));
    expect(screen.queryByText("Your agent's candidates")).not.toBeInTheDocument();
  });
});

describe('ProjectView highlight', () => {
  it('marks the rail the agent points at', async () => {
    render(<ProjectView params={{ project: 'Sensor board', highlight: '3V3' }} />);
    await screen.findByRole('heading', { name: /^Sensor board/ });
    expect(screen.getByText(/other loads 50 mA/).closest('.rail-card')).toHaveClass('agent-hl');
  });

  it('marks a BOM entry by designator', async () => {
    render(<ProjectView params={{ project: 'Sensor board', highlight: 'U2' }} />);
    await screen.findByRole('heading', { name: /^Sensor board/ });
    expect(document.querySelector('tr.agent-hl')).toHaveTextContent('SD1235');
  });
});

describe('agent presentation in the app', () => {
  it("shows the agent's explanation beside what it showed, with links", async () => {
    render(<App />);
    await screen.findByText('SD1234');
    agentShows({ kind: 'compare', parts: ['SD1234', 'TPX5430'], highlight: 'SD1234', explain: 'SD1234 switches at a fixed [650 kHz](#page/SD1234/2).' });
    const panel = await screen.findByRole('complementary', { name: "Agent's explanation" });
    expect(panel).toHaveTextContent('switches at a fixed 650 kHz');
    fireEvent.click(within(panel).getByText('650 kHz'));
    await waitFor(() => expect(useStore.getState().tabs.at(-1)).toMatchObject({ kind: 'page', params: { ds: 1, page: 1 } }));
  });

  it('walks through steps', async () => {
    render(<App />);
    await screen.findByText('SD1234');
    agentShows({
      kind: 'walkthrough',
      title: 'Why SD1234',
      steps: [
        { kind: 'part', part: 'SD1234', title: 'The part', explain: 'A 3 A buck.' },
        { kind: 'project', project: 'Sensor board', highlight: '3V3', title: 'Its rail' },
      ],
    });
    const bar = await screen.findByRole('region', { name: 'Walkthrough' });
    expect(bar).toHaveTextContent('Why SD1234');
    expect(bar).toHaveTextContent('Step 1 of 2');
    expect(await screen.findByRole('complementary', { name: "Agent's explanation" })).toHaveTextContent('A 3 A buck.');
    fireEvent.click(within(bar).getByRole('button', { name: /Next/ }));
    expect(bar).toHaveTextContent('Step 2 of 2');
    expect(await screen.findByRole('heading', { name: /^Sensor board/ })).toBeInTheDocument();
    act(() => {
      fireEvent.keyDown(window, { key: 'ArrowLeft', altKey: true });
    });
    expect(bar).toHaveTextContent('Step 1 of 2');
    fireEvent.click(within(bar).getByRole('button', { name: /End the walkthrough/ }));
    expect(screen.queryByRole('region', { name: 'Walkthrough' })).not.toBeInTheDocument();
  });

  it("asks first when the user doesn't follow the agent", async () => {
    render(<App />);
    await screen.findByText('SD1234');
    const app = useStore.getState().app!;
    useStore.setState({ app: { ...app, config: { ...app.config, settings: { ...app.config.settings, followAgent: false } } } });
    agentShows({ kind: 'walkthrough', title: 'Why SD1234', steps: [{ kind: 'part', part: 'SD1234' }] });
    expect(useStore.getState().walkthrough).toBeUndefined();
    const t = useStore.getState().toasts.at(-1)!;
    expect(t.title).toBe('Agent wants to walk you through: Why SD1234');
    act(() => t.action!.run());
    expect(useStore.getState().walkthrough?.index).toBe(0);
  });
});
