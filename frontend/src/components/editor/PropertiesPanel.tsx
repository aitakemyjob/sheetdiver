import type { EditorController, EditorSnapshot } from '../../lib/editor/controller';
import { describe, fieldsOf, type Field } from '../../lib/editor/fields';
import type { Element } from '../../lib/editor/elements';

/** One field's input. */
function FieldInput({ field, e, onChange }: { field: Field; e: Element; onChange: (e: Element) => void }) {
  const v = field.get(e);
  switch (field.kind) {
    case 'bool':
      return <input type="checkbox" checked={!!v} onChange={(x) => onChange(field.set(e, x.target.checked))} />;
    case 'select':
      return (
        <select value={String(v)} onChange={(x) => onChange(field.set(e, x.target.value))}>
          {field.options?.map((o) => (
            <option key={o}>{o}</option>
          ))}
        </select>
      );
    case 'number':
      return <input type="number" step="any" value={Number(v)} onChange={(x) => x.target.value !== '' && onChange(field.set(e, Number(x.target.value)))} />;
  }
  return <input type="text" value={String(v)} onChange={(x) => onChange(field.set(e, x.target.value))} />;
}

/** What's selected, its fields to change. */
export function PropertiesPanel({ editor, snap }: { editor: EditorController; snap: EditorSnapshot }) {
  const sel = snap.selection.filter((i) => snap.doc.elements[i]);
  if (sel.length === 0) return <div className="muted ed-side-empty">Select something to change it here.</div>;
  if (sel.length > 1) return <div className="muted ed-side-empty">{sel.length} things selected: drag to move them, R turns them, Delete removes them.</div>;
  const i = sel[0];
  const e = snap.doc.elements[i];
  return (
    <div className="ed-props">
      <b>{describe(e)}</b>
      {fieldsOf(e, snap.doc.layers).map((f) => (
        <label key={f.label} className="ed-field">
          <span>{f.label}</span>
          <FieldInput field={f} e={e} onChange={(next) => editor.update(i, next)} />
        </label>
      ))}
    </div>
  );
}
