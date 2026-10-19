import { describe, expect, it } from 'vitest';
import { finished, pool } from './store';
import { jobProgress, queueCounts } from '../components/ImportQueue';
import { splitList } from '../components/ImportDialog';
import { discouraged, statusInfo } from '../components/PartStatus';
import type { ImportJob } from './types';

const job = (p: Partial<ImportJob>): ImportJob => ({ id: 1, name: 'a.pdf', source: '/x/a.pdf', status: 'queued', done: 0, total: 0, started: '', finished: '', ...p });

describe('import queue', () => {
  it('knows when a job has ended', () => {
    expect(finished(job({ status: 'running' }))).toBe(false);
    expect(finished(job({ status: 'queued' }))).toBe(false);
    for (const st of ['done', 'existing', 'failed'] as const) expect(finished(job({ status: st }))).toBe(true);
  });

  it('counts jobs by state', () => {
    const c = queueCounts([job({ status: 'running' }), job({ status: 'queued' }), job({ status: 'done' }), job({ status: 'failed' }), job({ status: 'existing' })]);
    expect(c).toEqual({ active: 2, done: 1, existing: 1, failed: 1 });
  });

  it('describes progress', () => {
    expect(jobProgress(job({ status: 'running', stage: 'reading', done: 4, total: 40 }))).toBe('reading · page 4 of 40');
    expect(jobProgress(job({ status: 'done', parts: ['TPS563200', 'TPS562200'] }))).toBe('imported as TPS563200, TPS562200');
    expect(jobProgress(job({ status: 'existing' }))).toBe('already in the library');
    expect(jobProgress(job({ status: 'failed', error: 'not a PDF file' }))).toBe('not a PDF file');
  });

  it('runs a bounded pool', async () => {
    let inFlight = 0;
    let peak = 0;
    const seen: number[] = [];
    await pool([1, 2, 3, 4, 5, 6, 7], 3, async (n) => {
      inFlight++;
      peak = Math.max(peak, inFlight);
      await new Promise((r) => setTimeout(r, 2));
      seen.push(n);
      inFlight--;
    });
    expect(peak).toBe(3);
    expect(seen.sort()).toEqual([1, 2, 3, 4, 5, 6, 7]);
    await pool([], 3, async () => {}); // nothing to do is fine
  });
});

describe('import lists', () => {
  it('splits pasted part numbers and URLs', () => {
    expect(splitList('TPS563200\nMAX77801, STM32U575 ;TPS563200\n\n')).toEqual(['TPS563200', 'MAX77801', 'STM32U575']);
    expect(splitList('https://a/x.pdf https://b/y.pdf')).toEqual(['https://a/x.pdf', 'https://b/y.pdf']);
    expect(splitList('  ')).toEqual([]);
  });
});

describe('part statuses', () => {
  it('flags discouraged statuses', () => {
    expect(['avoid', 'nrnd', 'eol'].every(discouraged)).toBe(true);
    expect(discouraged('preferred') || discouraged('approved') || discouraged(undefined)).toBe(false);
    expect(Object.keys(statusInfo).sort()).toEqual(['approved', 'avoid', 'eol', 'nrnd', 'preferred']);
  });
});

describe('import events', () => {
  it('tracks jobs and summarizes a batch when it drains', async () => {
    const { useStore } = await import('./store');
    const st = useStore.getState;
    const ev = (j: Partial<ImportJob>) => st().handleEvent({ type: 'import', time: '', data: job({ batch: 'b1', ...j }) });
    const v0 = st().libraryVersion;
    ev({ id: 1, status: 'queued' });
    ev({ id: 2, status: 'queued' });
    ev({ id: 1, status: 'running', stage: 'reading' });
    expect(st().jobs[1].stage).toBe('reading');
    ev({ id: 1, status: 'done', parts: ['ZX1'] });
    expect(st().libraryVersion).toBe(v0 + 1);
    expect(st().toasts.some((t) => t.title.startsWith('Imported'))).toBe(false);
    ev({ id: 2, status: 'running' });
    ev({ id: 2, status: 'failed', error: 'no datasheet' });
    const toast = st().toasts.find((t) => t.title === 'Imported 1 of 2 datasheets');
    expect(toast?.kind).toBe('warn');
    expect(toast?.body).toBe('1 failed');
    ev({ id: 3, status: 'running', batch: 'b2' });
    st().handleEvent({ type: 'import', time: '', data: { cleared: true } });
    expect(Object.keys(st().jobs)).toEqual(['3']);
  });

  it('queues dropped folders recursively', async () => {
    const { useStore } = await import('./store');
    const calls: unknown[] = [];
    useStore.setState({ queueImports: async (r) => void calls.push(r), importPaths: async (p) => void calls.push(p) });
    useStore.getState().handleEvent({ type: 'drop', time: '', data: { paths: ['/d/a.pdf', '/d/sheets', '/d/notes.txt'] } });
    expect(calls).toEqual([['/d/a.pdf'], { folder: '/d/sheets', recursive: true }]);
  });
});
