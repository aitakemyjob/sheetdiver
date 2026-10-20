import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { ViewBoundary } from './ViewBoundary';

let crash = true;
function Fragile() {
  if (crash) throw new Error("Cannot read properties of null (reading 'filter')");
  return <div>project view</div>;
}

describe('ViewBoundary', () => {
  it('shows a crash in its tab and can try again', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
    render(
      <>
        <ViewBoundary>
          <Fragile />
        </ViewBoundary>
        <div>sidebar</div>
      </>,
    );
    expect(screen.getByText('This view ran into a problem')).toBeInTheDocument();
    expect(screen.getByText(/reading 'filter'/)).toBeInTheDocument();
    expect(screen.getByText('sidebar')).toBeInTheDocument();
    crash = false;
    fireEvent.click(screen.getByRole('button', { name: /Try again/ }));
    expect(screen.getByText('project view')).toBeInTheDocument();
  });
});
