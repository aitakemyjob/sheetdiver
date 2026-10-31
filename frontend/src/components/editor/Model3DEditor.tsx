import { useMemo, useRef, useState, type PointerEvent, type WheelEvent } from 'react';
import { Box, Copy, Cylinder, Redo2, RotateCcw, Save, Trash2, Undo2 } from 'lucide-react';
import { cadActions } from '../../lib/cad';
import { Model3DController, draw, frameOf, homeCamera, orbit, projector, shapeFieldsOf, topCamera, type Camera } from '../../lib/editor/model3d';
import type { FootprintModel, Pt, Shape3D } from '../../lib/editor/model';
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

/** How the view is turned, moved and zoomed. */
interface View {
  camera: Camera;
  // pan moves the middle of the view, in drawing units; zoom 1 frames the model.
  pan: Pt;
  zoom: number;
}

const homeView: View = { camera: homeCamera, pan: { x: 0, y: 0 }, zoom: 1 };

/** A drag in the view: turning it, or moving it; moved once it went past a click. */
interface Drag {
  at: Pt;
  mode: 'orbit' | 'pan';
  moved: boolean;
  shape: number;
}

/**
 * The 3D model editor: the model's shapes, each sized and placed in mm
 * (footprint coordinates, z up from the board), over its footprint, with
 * a live view to turn (drag), move (right or shift drag) and zoom (wheel)
 * where a click picks a shape.
 */
export function Model3DEditor({ part, pkg, shapes, footprint, edited, onSaved, onError }: { part: string; pkg: string; shapes: Shape3D[]; footprint?: FootprintModel; edited: boolean; onSaved: () => void; onError: (e: Error) => void }) {
  const editor = useMemo(() => new Model3DController(shapes), [shapes]);
  const snap = useStoreOf(editor);
  const [saving, setSaving] = useState(false);
  const [view, setView] = useState<View>(homeView);
  const [showFootprint, setShowFootprint] = useState(true);
  const svg = useRef<SVGSVGElement>(null);
  const drag = useRef<Drag | null>(null);
  const fp = showFootprint ? footprint : undefined;
  const faces = useMemo(() => draw(snap.shapes, view.camera, fp), [snap.shapes, view.camera, fp]);
  // Framed on the model as it opened, so editing a shape doesn't move the view.
  const frame = useMemo(() => frameOf(shapes, footprint), [shapes, footprint]);
  const middle = projector(view.camera).project(frame.centre)[0];
  const size = (2.2 * frame.radius) / view.zoom;
  const cx = middle.x + view.pan.x;
  const cy = middle.y + view.pan.y;
  // Drawing units per screen pixel: the view box fits the shorter side.
  const perPixel = () => {
    const r = svg.current?.getBoundingClientRect();
    return r && r.width && r.height ? size / Math.min(r.width, r.height) : 0.01;
  };
  const down = (e: PointerEvent<SVGSVGElement>) => {
    const shape = Number((e.target as Element).getAttribute('data-shape') ?? -1);
    drag.current = { at: { x: e.clientX, y: e.clientY }, mode: e.button === 0 && !e.shiftKey ? 'orbit' : 'pan', moved: false, shape };
    e.currentTarget.setPointerCapture(e.pointerId);
  };
  const move = (e: PointerEvent<SVGSVGElement>) => {
    const d = drag.current;
    if (!d) return;
    const [dx, dy] = [e.clientX - d.at.x, e.clientY - d.at.y];
    if (!d.moved && Math.hypot(dx, dy) < 3) return;
    d.moved = true;
    d.at = { x: e.clientX, y: e.clientY };
    if (d.mode === 'orbit') setView((v) => ({ ...v, camera: orbit(v.camera, -0.4 * dx, 0.4 * dy) }));
    else {
      const k = perPixel();
      setView((v) => ({ ...v, pan: { x: v.pan.x - dx * k, y: v.pan.y - dy * k } }));
    }
  };
  const up = () => {
    const d = drag.current;
    drag.current = null;
    if (d && !d.moved && d.mode === 'orbit' && d.shape >= 0) editor.select(d.shape);
  };
  // Zooms about the pointer: the point under it stays there.
  const wheel = (e: WheelEvent<SVGSVGElement>) => {
    const ctm = svg.current?.getScreenCTM();
    if (!ctm) return;
    const p = new DOMPoint(e.clientX, e.clientY).matrixTransform(ctm.inverse());
    const f = e.deltaY > 0 ? 1 / 1.15 : 1.15;
    const zoom = Math.min(50, Math.max(0.2, view.zoom * f));
    const g = zoom / view.zoom;
    setView({ ...view, zoom, pan: { x: p.x + (cx - p.x) / g - middle.x, y: p.y + (cy - p.y) / g - middle.y } });
  };
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
          {footprint && (
            <label className="row" style={{ gap: 4, fontSize: 12 }}>
              <input type="checkbox" checked={showFootprint} onChange={(e) => setShowFootprint(e.target.checked)} /> Footprint
            </label>
          )}
          <button className="btn sm ghost" onClick={() => setView({ ...homeView, camera: topCamera })} title="Look straight down">
            Top
          </button>
          <button className="btn sm ghost" onClick={() => setView(homeView)} title="Back to the starting view">
            Reset view
          </button>
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
      status="Sizes in mm: x right, y down as on the footprint, z up from the board. Drag to turn, right or shift drag to move, wheel to zoom; click a face to pick its shape."
    >
      <svg
        ref={svg}
        className="cad-drawing ed-canvas ed-3d"
        viewBox={`${cx - size / 2} ${cy - size / 2} ${size} ${size}`}
        role="img"
        aria-label="3D model"
        onPointerDown={down}
        onPointerMove={move}
        onPointerUp={up}
        onPointerCancel={up}
        onWheel={wheel}
        onContextMenu={(e) => e.preventDefault()}
      >
        {faces.map((f, i) => {
          const pts = f.pts.map((p) => `${p.x},${p.y}`).join(' ');
          if (f.line) return <polyline key={i} points={pts} style={{ fill: 'none', stroke: f.color, strokeWidth: f.line }} />;
          return <polygon key={i} data-shape={f.shape} className={`cad-face${f.shape >= 0 && f.shape === snap.selected ? ' ed-face-selected' : ''}`} points={pts} style={{ fill: f.color, stroke: f.color }} />;
        })}
      </svg>
    </EditorLayout>
  );
}
