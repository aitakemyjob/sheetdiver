import { Circle, CircleDot, MousePointer2, Maximize, PenLine, Pentagon, Pin, Redo2, Spline, Square, Trash2, Type, Undo2 } from 'lucide-react';
import { grids, type EditorController, type EditorSnapshot } from '../../lib/editor/controller';
import type { ToolName } from '../../lib/editor/tools';

/** Each tool's icon and name. */
const toolLooks: Record<ToolName, { icon: typeof Square; label: string; key: string }> = {
  select: { icon: MousePointer2, label: 'Select', key: 'V' },
  pad: { icon: CircleDot, label: 'Pad', key: 'P' },
  pin: { icon: Pin, label: 'Pin', key: 'P' },
  line: { icon: PenLine, label: 'Line', key: 'L' },
  rect: { icon: Square, label: 'Rectangle', key: 'B' },
  circle: { icon: Circle, label: 'Circle', key: 'C' },
  arc: { icon: Spline, label: 'Arc', key: 'A' },
  poly: { icon: Pentagon, label: 'Polygon', key: 'G' },
  text: { icon: Type, label: 'Text', key: 'T' },
};

/** The editor's tools, layer, grid and undo. */
export function EditorToolbar({ editor, snap }: { editor: EditorController; snap: EditorSnapshot }) {
  return (
    <>
      <div className="seg" role="toolbar" aria-label="Tools">
        {snap.tools.map((t) => {
          const l = toolLooks[t];
          const Icon = l.icon;
          return (
            <button key={t} className={snap.tool === t ? 'on' : ''} aria-pressed={snap.tool === t} title={l.label} onClick={() => editor.setTool(t)}>
              <Icon size={14} /> {l.label}
            </button>
          );
        })}
      </div>
      {snap.doc.layers.length > 1 && (
        <label className="row" style={{ gap: 4 }}>
          Layer
          <select value={snap.layer} onChange={(e) => editor.setLayer(e.target.value)}>
            {snap.doc.layers.map((l) => (
              <option key={l}>{l}</option>
            ))}
          </select>
        </label>
      )}
      <label className="row" style={{ gap: 4 }}>
        Grid
        <select value={snap.grid} onChange={(e) => editor.setGrid(Number(e.target.value))}>
          {grids[snap.doc.kind].map((g) => (
            <option key={g} value={g}>
              {g === 0 ? 'off' : `${g} mm`}
            </option>
          ))}
        </select>
      </label>
      <span className="grow" />
      <button className="btn sm ghost" title="Undo (Ctrl+Z)" disabled={!snap.canUndo} onClick={() => editor.undo()}>
        <Undo2 size={14} />
      </button>
      <button className="btn sm ghost" title="Redo (Ctrl+Shift+Z)" disabled={!snap.canRedo} onClick={() => editor.redo()}>
        <Redo2 size={14} />
      </button>
      <button className="btn sm ghost" title="Delete what's selected" disabled={!snap.selection.length} onClick={() => editor.removeSelected()}>
        <Trash2 size={14} />
      </button>
      <button className="btn sm ghost" title="Fit the drawing" onClick={() => editor.fitView()}>
        <Maximize size={14} />
      </button>
    </>
  );
}
