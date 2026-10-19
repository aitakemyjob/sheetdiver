import { useMemo } from 'react';
import { AlertCircle, CheckCircle2, Clock, Copy, ListChecks, RotateCw } from 'lucide-react';
import { api } from '../lib/api';
import { finished, useStore } from '../lib/store';
import type { ImportJob } from '../lib/types';
import { Modal } from './ui';

/** Counts of the import queue's jobs by state. */
export function queueCounts(jobs: ImportJob[]) {
  const c = { active: 0, done: 0, existing: 0, failed: 0 };
  for (const j of jobs) {
    if (j.status === 'queued' || j.status === 'running') c.active++;
    else c[j.status]++;
  }
  return c;
}

/** One-line progress of a job. */
export function jobProgress(j: ImportJob): string {
  if (j.status === 'queued') return 'queued';
  if (j.status === 'running') return `${j.stage ?? 'starting'}${j.total ? ` · page ${j.done} of ${j.total}` : ''}`;
  if (j.status === 'failed') return j.error ?? 'failed';
  return `${j.status === 'existing' ? 'already in the library' : 'imported'}${j.parts?.length ? ` as ${j.parts.join(', ')}` : ''}`;
}

/** The import queue: every import of this session with its progress, outcome and a retry for failures. */
export function ImportQueue() {
  const s = useStore();
  const jobs = useMemo(() => Object.values(s.jobs).sort((a, b) => order(a) - order(b) || b.id - a.id), [s.jobs]);
  const c = queueCounts(jobs);
  const failed = jobs.filter((j) => j.status === 'failed');
  const retry = (j: ImportJob) => api.retryImport(j.id).catch((e) => s.toast({ kind: 'err', title: 'Could not retry', body: (e as Error).message }));
  const copyFailed = () => navigator.clipboard?.writeText(failed.map((j) => `${j.source}\t${j.error ?? ''}`).join('\n'));
  return (
    <Modal
      wide
      title="Imports"
      icon={<ListChecks size={16} style={{ color: 'var(--accent)' }} />}
      onClose={() => s.setQueueOpen(false)}
      footer={
        <>
          <span className="faint grow">
            {c.active > 0 && `${c.active} in progress · `}
            {c.done} imported · {c.existing} already in the library · {c.failed} failed
          </span>
          {failed.length > 0 && (
            <>
              <button className="btn sm" onClick={copyFailed} title="Copy the failed sources and errors">
                <Copy size={13} /> Copy failures
              </button>
              <button className="btn sm" onClick={() => failed.filter(retriable).forEach(retry)} disabled={!failed.some(retriable)}>
                <RotateCw size={13} /> Retry failed
              </button>
            </>
          )}
          <button className="btn sm" disabled={jobs.length === c.active} onClick={() => api.clearImports()}>
            Clear finished
          </button>
          <button className="btn sm primary" onClick={() => s.setImportOpen(true)}>
            Import more
          </button>
        </>
      }
    >
      {jobs.length === 0 ? (
        <div className="faint" style={{ padding: 20, textAlign: 'center' }}>
          No imports yet. Drop PDFs or a folder anywhere, or use Import.
        </div>
      ) : (
        <div className="queue">
          {jobs.map((j) => (
            <div key={j.id} className={`queue-row ${j.status}`}>
              <JobIcon j={j} />
              <div className="grow" style={{ minWidth: 0 }}>
                <div className="ellipsis" title={j.source}>
                  <b>{j.name}</b>
                  {j.source !== j.name && <span className="faint"> · {j.source}</span>}
                </div>
                <div className={`ellipsis ${j.status === 'failed' ? 'err-text' : 'faint'}`} title={jobProgress(j)}>
                  {jobProgress(j)}
                </div>
                {j.status === 'running' && (
                  <div className="bar">
                    <i style={{ width: `${j.total ? (100 * j.done) / j.total : 8}%` }} />
                  </div>
                )}
              </div>
              {j.status === 'failed' && retriable(j) && (
                <button className="btn sm ghost" onClick={() => retry(j)}>
                  <RotateCw size={12} /> Retry
                </button>
              )}
              {finished(j) && j.parts?.[0] && (
                <button
                  className="btn sm ghost"
                  onClick={() => {
                    s.setQueueOpen(false);
                    s.open({ kind: 'part', title: j.parts![0], params: { part: j.parts![0] } });
                  }}
                >
                  Open
                </button>
              )}
            </div>
          ))}
        </div>
      )}
    </Modal>
  );
}

/** Sort order: running, queued, failed, then finished. */
function order(j: ImportJob): number {
  return { running: 0, queued: 1, failed: 2, done: 3, existing: 3 }[j.status];
}

/** Reports whether a failed job can run again (uploads can't). */
function retriable(j: ImportJob): boolean {
  return j.source !== 'upload';
}

/** State icon of a job. */
function JobIcon({ j }: { j: ImportJob }) {
  switch (j.status) {
    case 'running':
      return <div className="spinner" style={{ width: 14, height: 14, borderWidth: 2, flex: 'none' }} />;
    case 'queued':
      return <Clock size={15} className="faint" style={{ flex: 'none' }} />;
    case 'failed':
      return <AlertCircle size={15} style={{ color: 'var(--err)', flex: 'none' }} />;
    default:
      return <CheckCircle2 size={15} style={{ color: j.status === 'done' ? 'var(--ok)' : 'var(--text-3)', flex: 'none' }} />;
  }
}
