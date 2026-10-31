import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { fakeBackend, type FakeBackend } from '../test/backend';
import { PartCadPanel } from './PartCad';
import { SettingsView } from './SettingsView';

let b: FakeBackend;
beforeEach(() => {
  b = fakeBackend();
});
afterEach(cleanup);

describe('the CAD tab', () => {
  it('shows each package with its symbol, footprint and 3D model, where each came from, and resets an edit', async () => {
    render(<PartCadPanel part="SD1234" />);
    const pkg = await screen.findByRole('region', { name: 'Package SOT-23-6' });
    expect(within(pkg).getByRole('img', { name: 'SD1234 symbol' })).toBeTruthy();
    expect(within(pkg).getByRole('img', { name: 'SD1234 footprint' })).toBeTruthy();
    expect(within(pkg).getByRole('img', { name: 'SD1234 3D model' })).toBeTruthy();
    expect(within(pkg).getByText('Your edit')).toBeTruthy();
    expect(within(pkg).getByText('Agent')).toBeTruthy();
    expect(within(pkg).getByText('Land pattern from page 31')).toBeTruthy();
    expect(within(pkg).getByRole('table', { name: 'Package outline' })).toBeTruthy();
    window.confirm = () => true;
    fireEvent.click(within(pkg).getByRole('button', { name: /Discard my edit/ }));
    await waitFor(() => expect(b.callsTo('cad.reset')).toEqual([{ part: 'SD1234', package: 'SOT-23-6', kind: 'symbol' }]));
  });

  it('says what a part without a package needs', async () => {
    render(<PartCadPanel part="TPX5430" />);
    expect(await screen.findByText(/package isn’t known|package isn't known/)).toBeTruthy();
  });

  it('syncs the library and says what it kept', async () => {
    render(<PartCadPanel part="SD1234" />);
    fireEvent.click(await screen.findByRole('button', { name: /Sync library/ }));
    await waitFor(() => expect(b.callsTo('cad.sync').length).toBe(1));
  });
});

describe('the CAD library settings', () => {
  it('chooses the formats', async () => {
    render(<SettingsView />);
    const formats = await screen.findByRole('group', { name: 'CAD formats' });
    fireEvent.click(within(formats).getByRole('checkbox', { name: /Eagle/ }));
    await waitFor(() => expect((b.callsTo('settings.update').at(-1) as { cadFormats?: string[] } | undefined)?.cadFormats).toEqual(['eagle', 'kicad']));
    expect(screen.getByText(/Manage Symbol Libraries/)).toBeTruthy();
  });
});
