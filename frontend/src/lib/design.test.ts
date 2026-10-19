import { afterEach, describe, expect, it, vi } from 'vitest';
import { saveText } from './api';
import { setTransport, type Transport } from './transport';
import { directionHint, verdictInfo } from '../views/Alternatives';
import { reportFilename } from '../components/ReportDialog';
import { updateSummary } from '../components/UpdateDialog';
import type { UpdateCheck } from './types';

describe('alternatives', () => {
  it('explains each direction', () => {
    expect(directionHint({ direction: 'higher' })).toBe('higher is better');
    expect(directionHint({ direction: 'cover' })).toMatch(/cover/);
    expect(Object.keys(verdictInfo).sort()).toEqual(['better', 'same', 'unknown', 'worse']);
  });
});

describe('reports', () => {
  it('names report files safely', () => {
    expect(reportFilename('Sensor | board v2')).toBe('Sensor-board-v2-design-review.md');
    expect(reportFilename('///')).toBe('project-design-review.md');
  });
});

describe('datasheet updates', () => {
  const base: UpdateCheck = { datasheetId: 1, filename: 'x.pdf', source: 'https://x', current: 'SLVSCB0 rev D', upToDate: false, recorded: 0 };
  it('summarizes a check', () => {
    expect(updateSummary({ ...base, upToDate: true })).toBe('Up to date (SLVSCB0 rev D)');
    expect(updateSummary({ ...base, error: 'timeout' })).toBe('Could not check: timeout');
    expect(updateSummary({ ...base, latest: 'SLVSCB0 rev E', counts: { changed: 2, removed: 1 } })).toBe(
      'New revision SLVSCB0 rev E (have SLVSCB0 rev D): 2 changed, 0 added, 1 removed spec rows',
    );
  });
});

describe('saveText', () => {
  afterEach(() => setTransport(undefined));

  it('uses the desktop save dialog with UTF-8 base64', async () => {
    const calls: [string, unknown][] = [];
    const t: Transport = {
      kind: 'desktop',
      async invoke<T>(m: string, a?: unknown): Promise<T> {
        calls.push([m, a]);
        return { path: '/home/me/r.md' } as T;
      },
      subscribe: () => () => {},
    };
    setTransport(t);
    expect(await saveText('r.md', 'Ω µA', 'text/markdown', true)).toBe('/home/me/r.md');
    const [method, args] = calls[0] as [string, { name: string; data: string }];
    expect(method).toBe('file.save');
    expect(new TextDecoder().decode(Uint8Array.from(atob(args.data), (c) => c.charCodeAt(0)))).toBe('Ω µA');
  });

  it('downloads in the browser', async () => {
    const click = vi.fn();
    const orig = document.createElement.bind(document);
    vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
      const el = orig(tag);
      if (tag === 'a') el.click = click;
      return el;
    });
    URL.createObjectURL = vi.fn(() => 'blob:x');
    URL.revokeObjectURL = vi.fn();
    expect(await saveText('c.csv', 'a,b', 'text/csv', false)).toBe('c.csv');
    expect(click).toHaveBeenCalled();
    vi.restoreAllMocks();
  });
});
