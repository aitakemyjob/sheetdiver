import { useMemo, useState } from 'react';
import { Save } from 'lucide-react';
import { cadActions, problemsLine } from '../../lib/cad';
import { EditorController } from '../../lib/editor/controller';
import { documentFor, type EditKind } from '../../lib/editor/factory';
import type { FootprintModel, SymbolModel } from '../../lib/editor/model';
import type { CadProblem } from '../../lib/types';
import { EditorCanvas } from './EditorCanvas';
import { EditorLayout } from './EditorLayout';
import { EditorToolbar } from './EditorToolbar';
import { PropertiesPanel } from './PropertiesPanel';
import { useStoreOf } from './hooks';

/**
 * The symbol or footprint editor: draw and change it freely, then keep
 * it as your edit (written to every library format).
 */
export function DrawingEditor({ part, pkg, kind, model, onSaved, onError }: { part: string; pkg: string; kind: EditKind; model: SymbolModel | FootprintModel; onSaved: (problems: CadProblem[]) => void; onError: (e: Error) => void }) {
  const editor = useMemo(() => new EditorController(documentFor(kind, model)), [kind, model]);
  const snap = useStoreOf(editor);
  const [saving, setSaving] = useState(false);
  const [problems, setProblems] = useState<CadProblem[]>([]);
  const save = () => {
    setSaving(true);
    cadActions
      .save(part, pkg, kind, editor.model())
      .then((r) => {
        editor.saved(editor.doc);
        setProblems(r.problems);
        onSaved(r.problems);
      })
      .catch(onError)
      .finally(() => setSaving(false));
  };
  return (
    <EditorLayout
      title={
        <>
          <b>{part}</b> {kind} · {pkg || 'package'}
          {snap.dirty && <span className="muted"> · not saved</span>}
        </>
      }
      actions={
        <button className="btn primary sm" disabled={!snap.dirty || saving} onClick={save} title="Keep it as your edit and write the library">
          <Save size={14} /> {saving ? 'Saving…' : 'Save'}
        </button>
      }
      toolbar={<EditorToolbar editor={editor} snap={snap} />}
      side={<PropertiesPanel editor={editor} snap={snap} />}
      status={problems.length ? <span className="warn-text">{problemsLine(problems)}</span> : snap.hint}
    >
      <EditorCanvas editor={editor} snap={snap} />
    </EditorLayout>
  );
}
