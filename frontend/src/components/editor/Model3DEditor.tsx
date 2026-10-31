import { useMemo, useState } from 'react';
import { Box, Copy, Cylinder, Redo2, RotateCcw, Save, Trash2, Undo2 } from 'lucide-react';
import { cadActions } from '../../lib/cad';
import { Model3DController, shapeFieldsOf, viewOf } from '../../lib/editor/model3d';
import type { Shape3D } from '../../lib/editor/model';
import { EditorLayout } from './EditorLayout';
import { useStoreOf } from './hooks';

/** A shape's fields. */
function ShapeFields({ shape, onChange }: { shape: Shape3D; onChange: (s: Shape3D) => void }) {
  return (
    <div className="ed-props">
      <label className="ed-field">
        <span>Name</span>
        <input type="text" value={shape.name ?? ''} onChange={(e) => onChange({ ...shape, name: e.target.value })} />
      </label>
      {shapeFieldsOf(shape).map((f) => (
        <label key={f.label} className="ed-field">
          <span>{f.label}</span>
          {f.kind === 'select' ? (
            <select value={String(f.get(shape))} onChange={(e) => onChange(f.set(shape, e.target.value))}>
              {f.options?.map((o) => (
                <option key={o} value={o}>
                  {o || 'by part'}
                </option>
              ))}
            </select>
          ) : (
            <input type="number" step="any" value={Number(f.get(shape))} onChange={(e) => e.target.value !== '' && onChange(f.set(shape, Number(e.target.value)))} />
          )}
        </label>
      ))}
      {shape.kind === 'solid' && <div className="muted" style={{ fontSize: 12 }}>A solid as SheetDiver built it: move it, or replace it with boxes and cylinders.</div>}
    </div>
  );
}

/**
 * The 3D model editor: the model's shapes, each sized and placed in mm
 * (footprint coordinates, z up from the board), with a live view where a
 * click picks a shape.
 */
export function Model3DEditor({ part, pkg, shapes, edited, onSaved, onError }: { part: string; pkg: string; shapes: Shape3D[]; edited: boolean; onSaved: () => void; onError: (e: Error) => void }) {
  const editor = useMemo(() => new Model3DController(shapes), [shapes]);
  const snap = useStoreOf(editor);
  const [saving, setSaving] = useState(false);
  const v = viewOf(snap.faces);
  const sel = snap.shapes[snap.selected];
  const save = () => {
    setSaving(true);
    cadActions
      .save3d(part, pkg, snap.shapes)
      .then(() => (editor.saved(), onSaved()))
      .catch(onError)
      .finally(() => setSaving(false));
  };
  const reset = () => cadActions.reset3d(part, pkg, (m) => window.confirm(m))?.then(onSaved).catch(onError);
  return (
    <EditorLayout
      title={
        <>
          <b>{part}</b> 3D model · {pkg || 'package'}
          {snap.dirty && <span className="muted"> · not saved</span>}
        </>
      }
      actions={
        <>
          {edited && (
            <button className="btn ghost sm" onClick={reset} title="Build it from the package outline again">
              <RotateCcw size={14} /> Discard my 3D edit
            </button>
          )}
          <button className="btn primary sm" disabled={!snap.dirty || saving || !snap.shapes.length} onClick={save}>
            <Save size={14} /> {saving ? 'Saving…' : 'Save'}
          </button>
        </>
      }
      toolbar={
        <>
          <button className="btn sm" onClick={() => editor.add('box')}>
            <Box size={14} /> Box
          </button>
          <button className="btn sm" onClick={() => editor.add('cylinder')}>
            <Cylinder size={14} /> Cylinder
          </button>
          <button className="btn sm ghost" disabled={!sel} onClick={() => editor.duplicate(snap.selected)} title="Duplicate">
            <Copy size={14} />
          </button>
          <button className="btn sm ghost" disabled={!sel} onClick={() => editor.remove(snap.selected)} title="Remove">
            <Trash2 size={14} />
          </button>
          <span className="grow" />
          <button className="btn sm ghost" disabled={!snap.canUndo} onClick={() => editor.undo()} title="Undo">
            <Undo2 size={14} />
          </button>
          <button className="btn sm ghost" disabled={!snap.canRedo} onClick={() => editor.redo()} title="Redo">
            <Redo2 size={14} />
          </button>
        </>
      }
      side={
        <>
          <ul className="ed-shapes" aria-label="Shapes">
            {snap.shapes.map((s, i) => (
              <li key={i}>
                <button className={`btn ghost sm${i === snap.selected ? ' active' : ''}`} onClick={() => editor.select(i)}>
                  {s.name || s.kind} <span className="muted">{s.kind}</span>
                </button>
              </li>
            ))}
          </ul>
          {sel ? <ShapeFields shape={sel} onChange={(s) => editor.update(snap.selected, s)} /> : <div className="muted ed-side-empty">Pick a shape in the list or the view.</div>}
        </>
      }
      status="Sizes in mm: x right, y down as on the footprint, z up from the board. Click a face to pick its shape."
    >
      <svg className="cad-drawing ed-canvas" viewBox={`${v.x0} ${v.y0} ${v.x1 - v.x0} ${v.y1 - v.y0}`} role="img" aria-label="3D model">
        {snap.faces.map((f, i) => (
          <polygon key={i} className={`cad-face${f.shape === snap.selected ? ' ed-face-selected' : ''}`} points={f.pts.map((p) => `${p.x},${p.y}`).join(' ')} style={{ fill: f.color, stroke: f.color }} onClick={() => editor.select(f.shape)} />
        ))}
      </svg>
    </EditorLayout>
  );
}
