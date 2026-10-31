import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { useStore } from '../lib/store';
import { fakeBackend } from '../test/backend';
import { Sidebar } from './Sidebar';

beforeEach(() => {
  fakeBackend();
  localStorage.clear();
  useStore.setState({ sidebar: 'library' });
});

describe('the library tree', () => {
  it('groups parts by category under groups, which close and open', async () => {
    render(<Sidebar />);
    // Everything open at first: group, category, part.
    const power = await screen.findByText('Power');
    expect(await screen.findByText('SD1234')).toBeInTheDocument();
    // Closing the category hides its parts; closing the group hides its categories.
    fireEvent.click(screen.getByText('Buck'));
    expect(screen.queryByText('SD1234')).not.toBeInTheDocument();
    fireEvent.click(power);
    expect(screen.queryByText('Buck')).not.toBeInTheDocument();
    fireEvent.click(power);
    expect(screen.getByText('Buck')).toBeInTheDocument();
    expect(JSON.parse(localStorage.getItem('sheetdiver.library.closed') ?? '{}')).toMatchObject({ 'power/buck': true, power: false });
  });

  it('opens a part from the tree', async () => {
    render(<Sidebar />);
    fireEvent.click(await screen.findByText('SD1234'));
    expect(useStore.getState().tabs.some((t) => t.kind === 'part' && t.params.part === 'SD1234')).toBe(true);
  });

  it('shows every match of a filter with its tree open', async () => {
    render(<Sidebar />);
    await screen.findByText('Power');
    fireEvent.change(screen.getByPlaceholderText(/Filter by part number/), { target: { value: 'SD12' } });
    expect(await screen.findByText('SD1234')).toBeInTheDocument();
  });
});
