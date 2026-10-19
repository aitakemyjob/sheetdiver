import { useEffect, useState } from 'react';
import { CloudDownload } from 'lucide-react';
import { api } from '../lib/api';
import { useStore } from '../lib/store';
import type { Datasheet, UpdateCheck } from '../lib/types';
import { ErrorBox, Modal, Spinner } from './ui';

/** One-line summary of an update check. */
export function updateSummary(c: UpdateCheck): string {
  if (c.error) return `Could not check: ${c.error}`;
  if (c.upToDate) return `Up to date (${c.current})`;
  const n = c.counts ?? {};
  return `New revision ${c.latest ?? ''} (have ${c.current}): ${n.changed ?? 0} changed, ${n.added ?? 0} added, ${n.removed ?? 0} removed spec rows`;
}

/**
 * Checks a datasheet for a newer revision, shows what changed in its spec
 * tables and applies it on request.
 */
export function UpdateDialog({ ds, onClose }: { ds: Datasheet; onClose: () => void }) {
  const s = useStore();
  const [check, setCheck] = useState<UpdateCheck>();
  const [error, setError] = useState<unknown>();
  const [applying, setApplying] = useState(false);
  useEffect(() => {
    api.checkUpdate(ds.id).then(setCheck, setError);
  }, [ds.id]);
  const apply = async () => {
    setApplying(true);
    try {
      const r = await api.applyUpdate(ds.id);
      s.toast({ kind: 'ok', title: `Updated ${r.datasheet.filename}`, body: `Now ${r.datasheet.meta?.revision ?? 'the latest revision'}` });
      s.refreshLibrary();
      onClose();
    } catch (e) {
      setError(e);
    } finally {
      setApplying(false);
    }
  };
  const changed = check && !check.upToDate;
  return (
    <Modal
      wide
      title={`Datasheet revision · ${ds.filename}`}
      icon={<CloudDownload size={16} style={{ color: 'var(--accent)' }} />}
      onClose={onClose}
      footer={
        changed ? (
          <>
            <span className="faint grow">
              The datasheet keeps its parts and projects; extracted data is replaced{check.recorded ? `, and ${check.recorded} recorded values are kept (they may cite old page numbers)` : ''}.
            </span>
            <button className="btn sm" onClick={onClose}>
              Not now
            </button>
            <button className="btn sm primary" disabled={applying} onClick={apply}>
              {applying ? 'Updating…' : 'Update datasheet'}
            </button>
          </>
        ) : undefined
      }
    >
      <ErrorBox error={error} />
      {!check && !error && <Spinner label={`Downloading from ${ds.sourceUrl || 'the manufacturer'}…`} />}
      {check && (
        <div className="col">
          <div className="row" style={{ flexWrap: 'wrap', gap: 6 }}>
            <span className={`badge ${check.upToDate ? 'ok' : 'warn'}`}>{check.upToDate ? 'up to date' : 'new revision'}</span>
            <span>{updateSummary(check)}</span>
          </div>
          <div className="faint ellipsis" title={check.source}>
            Source: {check.source}
          </div>
          {changed && (check.changes?.length ?? 0) > 0 && (
            <div style={{ maxHeight: 380, overflow: 'auto' }}>
              <table className="dtable">
                <thead>
                  <tr>
                    <th />
                    <th>Parameter</th>
                    <th>Section</th>
                    <th>Before (min / typ / max)</th>
                    <th>After</th>
                  </tr>
                </thead>
                <tbody>
                  {check.changes!.map((c, i) => (
                    <tr key={i}>
                      <td>
                        <span className={`badge ${{ changed: 'warn', added: 'ok', removed: 'err' }[c.kind]}`}>{c.kind}</span>
                      </td>
                      <td>
                        {c.key && <span className="badge accent">{c.key}</span>} {c.name}
                        {c.conditions && <div className="faint" style={{ fontSize: 11 }}>{c.conditions}</div>}
                      </td>
                      <td className="faint">{c.section}</td>
                      <td className="mono" style={{ fontSize: 12 }}>
                        {c.before ?? '–'}
                      </td>
                      <td className="mono" style={{ fontSize: 12 }}>
                        {c.after ?? '–'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {changed && (check.changes?.length ?? 0) === 0 && <div className="faint">The PDF changed but no spec-table values did.</div>}
        </div>
      )}
    </Modal>
  );
}

/** Checks every datasheet for a newer revision and lists the outcome; each new revision can be reviewed and applied. */
export function AllUpdatesDialog({ onClose }: { onClose: () => void }) {
  const [checks, setChecks] = useState<UpdateCheck[]>();
  const [error, setError] = useState<unknown>();
  const [review, setReview] = useState<Datasheet>();
  useEffect(() => {
    api.checkAllUpdates().then(setChecks, setError);
  }, []);
  const order = (c: UpdateCheck) => (c.error ? 1 : c.upToDate ? 2 : 0);
  const sorted = [...(checks ?? [])].sort((a, b) => order(a) - order(b) || a.filename.localeCompare(b.filename));
  const fresh = sorted.filter((c) => !c.upToDate && !c.error).length;
  if (review) return <UpdateDialog ds={review} onClose={() => setReview(undefined)} />;
  return (
    <Modal wide title="Datasheet updates" icon={<CloudDownload size={16} style={{ color: 'var(--accent)' }} />} onClose={onClose}>
      <ErrorBox error={error} />
      {!checks && !error && <Spinner label="Downloading every datasheet from its source to compare…" />}
      {checks && (
        <div className="col">
          <div className="muted">
            {fresh ? `${fresh} datasheet${fresh === 1 ? ' has' : 's have'} a newer revision.` : 'Every datasheet that could be checked is up to date.'} {sorted.filter((c) => c.error).length} could not be checked.
          </div>
          <div className="queue">
            {sorted.map((c) => (
              <div key={c.datasheetId} className="queue-row">
                <span className={`badge ${c.error ? '' : c.upToDate ? 'ok' : 'warn'}`}>{c.error ? 'unchecked' : c.upToDate ? 'current' : 'new'}</span>
                <div className="grow" style={{ minWidth: 0 }}>
                  <div className="ellipsis">
                    <b>{c.filename}</b>
                  </div>
                  <div className={`ellipsis ${c.error ? 'err-text' : 'faint'}`} title={updateSummary(c)}>
                    {updateSummary(c)}
                  </div>
                </div>
                {!c.upToDate && !c.error && (
                  <button className="btn sm" onClick={() => api.datasheet(c.datasheetId).then((r) => setReview(r.datasheet), setError)}>
                    Review
                  </button>
                )}
              </div>
            ))}
          </div>
        </div>
      )}
    </Modal>
  );
}
