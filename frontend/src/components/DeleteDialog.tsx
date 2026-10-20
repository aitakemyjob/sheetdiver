import { useEffect, useState } from 'react';
import { Trash2 } from 'lucide-react';
import { api } from '../lib/api';
import { plural } from '../lib/format';
import { useStore } from '../lib/store';
import type { DeletePlan } from '../lib/types';
import { ErrorBox, Modal, Spinner, Switch } from './ui';

/** Describes recorded data that a delete loses, e.g. "3 parameters, 1 figure description". */
export function recordedText(recorded: Record<string, number>): string {
  const names: Record<string, [string, string]> = {
    parameters: ['parameter', 'parameters'],
    figures: ['figure description', 'figure descriptions'],
    tables: ['table transcription', 'table transcriptions'],
    pins: ['pin', 'pins'],
    registers: ['register', 'registers'],
    recommendations: ['recommended component', 'recommended components'],
  };
  return Object.entries(recorded)
    .filter(([, n]) => n > 0)
    .map(([k, n]) => `${n.toLocaleString()} ${(names[k] ?? [k, k])[n === 1 ? 0 : 1]}`)
    .join(', ');
}

/**
 * Confirms deleting a part from the library: with a datasheet, the
 * datasheet and every part on it go. Lists what will be removed (parts,
 * recorded data, notes, project use) before anything happens.
 */
export function DeleteDialog({ part, onClose, onDeleted }: { part: string; onClose: () => void; onDeleted: (removed: string[]) => void }) {
  const s = useStore();
  const [plan, setPlan] = useState<DeletePlan>();
  const [error, setError] = useState<unknown>();
  const [keep, setKeep] = useState(true);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    api.deletePreview(part).then(setPlan, setError);
  }, [part]);
  const apply = async () => {
    setBusy(true);
    try {
      const r = await api.deleteApply(part, keep);
      const what = r.datasheet ?? r.deleted.join(', ');
      s.toast({ kind: 'ok', title: `Deleted ${what}`, body: r.kept?.length ? `${r.kept.join(', ')} kept in projects without a datasheet` : undefined });
      s.refreshLibrary();
      useStore.setState({ projectVersion: useStore.getState().projectVersion + 1 });
      onDeleted(r.deleted);
    } catch (e) {
      setError(e);
      setBusy(false);
    }
  };
  const recorded = plan ? recordedText(plan.recorded) : '';
  const title = plan?.datasheet ? `Delete ${plan.datasheet.filename}?` : `Delete ${part}?`;
  return (
    <Modal
      title={title}
      icon={<Trash2 size={16} style={{ color: 'var(--err)' }} />}
      onClose={onClose}
      footer={
        <>
          <span className="faint grow">This can't be undone.</span>
          <button className="btn sm" onClick={onClose} autoFocus>
            Cancel
          </button>
          <button className="btn sm danger-solid" disabled={!plan || busy} onClick={apply}>
            <Trash2 size={13} /> {busy ? 'Deleting…' : 'Delete'}
          </button>
        </>
      }
    >
      <ErrorBox error={error} />
      {!plan && !error && <Spinner label="Checking what this removes…" />}
      {plan && (
        <div className="col">
          {plan.datasheet ? (
            <div>
              Removes the datasheet <b>{plan.datasheet.filename}</b> ({plural(plan.datasheet.pages, 'page')}) with everything extracted from it, and {plan.parts.length === 1 ? 'its part' : `its ${plan.parts.length} parts`}:
            </div>
          ) : (
            <div>Removes this part (it has no datasheet):</div>
          )}
          <ul className="delete-list">
            {plan.parts.map((u) => (
              <li key={u.part.id}>
                <b className="mono">{u.part.mpn}</b>
                {u.projects?.length ? <span className="badge warn">in {u.projects.join(', ')}</span> : null}
              </li>
            ))}
          </ul>
          {(recorded || plan.notes > 0) && (
            <div className="err-text">
              Also lost: {[recorded && `what you and agents recorded (${recorded})`, plan.notes > 0 && plural(plan.notes, 'note')].filter(Boolean).join(' and ')}.
            </div>
          )}
          {plan.inProjects && (
            <label className="row" style={{ gap: 8, alignItems: 'flex-start' }}>
              <Switch on={keep} onChange={setKeep} label="Keep parts used in projects" />
              <span>
                <b>Keep the parts used in projects</b>
                <div className="faint" style={{ fontSize: 12 }}>
                  {keep ? 'They stay in their projects without a datasheet (you can import one again later).' : 'They are removed from their projects too.'}
                </div>
              </span>
            </label>
          )}
        </div>
      )}
    </Modal>
  );
}
