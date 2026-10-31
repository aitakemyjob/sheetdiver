import { useEffect, useRef, type PointerEvent, type WheelEvent } from 'react';
import { behaviourOf } from '../../lib/editor/elements';
import type { EditorController, EditorSnapshot } from '../../lib/editor/controller';
import type { Pt } from '../../lib/editor/model';
import { ElementShape } from './ElementShape';

/** The grid's lines in a view, when there aren't too many. */
function gridLines(s: EditorSnapshot): { xs: number[]; ys: number[] } {
  const v = s.view;
  let g = s.grid;
  while (g > 0 && (v.x1 - v.x0) / g > 120) g *= 2;
  if (g <= 0) return { xs: [], ys: [] };
  const range = (a: number, b: number) => Array.from({ length: Math.floor(b / g) - Math.ceil(a / g) + 1 }, (_, i) => (Math.ceil(a / g) + i) * g);
  return { xs: range(v.x0, v.x1), ys: range(v.y0, v.y1) };
}

/**
 * The drawing being edited: its grid, elements, selection and handles,
 * and what a tool is drawing. Pointer, wheel and keys go to the editor
 * in drawing coordinates; the right or middle button pans.
 */
export function EditorCanvas({ editor, snap }: { editor: EditorController; snap: EditorSnapshot }) {
  const svg = useRef<SVGSVGElement>(null);
  const pan = useRef<Pt | null>(null);
  const v = snap.view;

  // The pick tolerance follows the zoom: about 6 screen pixels.
  useEffect(() => {
    const w = svg.current?.clientWidth || 800;
    editor.tol = (6 * (v.x1 - v.x0)) / w;
  }, [editor, v]);

  const at = (e: { clientX: number; clientY: number }): Pt => {
    const ctm = svg.current?.getScreenCTM();
    if (!ctm) return { x: 0, y: 0 };
    const p = new DOMPoint(e.clientX, e.clientY).matrixTransform(ctm.inverse());
    return { x: p.x, y: p.y };
  };
  const down = (e: PointerEvent<SVGSVGElement>) => {
    svg.current?.focus();
    svg.current?.setPointerCapture(e.pointerId);
    if (e.button === 1 || e.button === 2) {
      pan.current = at(e);
      return;
    }
    editor.pointerDown(at(e), e.shiftKey || e.ctrlKey || e.metaKey);
  };
  const move = (e: PointerEvent<SVGSVGElement>) => {
    const p = at(e);
    if (pan.current) {
      editor.panBy(p.x - pan.current.x, p.y - pan.current.y);
      return;
    }
    editor.pointerMove(p);
  };
  const up = (e: PointerEvent<SVGSVGElement>) => {
    if (pan.current) {
      pan.current = null;
      return;
    }
    editor.pointerUp(at(e));
  };
  const wheel = (e: WheelEvent<SVGSVGElement>) => editor.zoomAt(at(e), e.deltaY > 0 ? 1.15 : 1 / 1.15);
  const lines = gridLines(snap);
  const hs = 0.08 * Math.max(0.5, (v.x1 - v.x0) / 20);
  return (
    <svg
      ref={svg}
      className="cad-drawing ed-canvas"
      tabIndex={0}
      role="application"
      aria-label={`${snap.doc.kind} editor`}
      viewBox={`${v.x0} ${v.y0} ${v.x1 - v.x0} ${v.y1 - v.y0}`}
      preserveAspectRatio="xMidYMid meet"
      onPointerDown={down}
      onPointerMove={move}
      onPointerUp={up}
      onWheel={wheel}
      onContextMenu={(e) => e.preventDefault()}
      onKeyDown={(e) => editor.key(e.key, e.ctrlKey || e.metaKey, e.shiftKey) && e.preventDefault()}
    >
      <g className="ed-grid">
        {lines.xs.map((x) => (
          <line key={`x${x}`} x1={x} y1={v.y0} x2={x} y2={v.y1} />
        ))}
        {lines.ys.map((y) => (
          <line key={`y${y}`} x1={v.x0} y1={y} x2={v.x1} y2={y} />
        ))}
        <line className="ed-axis" x1={0} y1={v.y0} x2={0} y2={v.y1} />
        <line className="ed-axis" x1={v.x0} y1={0} x2={v.x1} y2={0} />
      </g>
      {snap.doc.elements.map((e, i) => (
        <ElementShape key={i} e={e} />
      ))}
      {snap.selection.map((i) => {
        const e = snap.doc.elements[i];
        if (!e) return null;
        const b = behaviourOf(e).box(e);
        return (
          <g key={`s${i}`} className="ed-selected">
            <rect x={b.x0} y={b.y0} width={b.x1 - b.x0} height={b.y1 - b.y0} />
            {behaviourOf(e)
              .handles(e)
              .map((h, k) => (
                <rect key={k} className="ed-handle" x={h.x - hs} y={h.y - hs} width={2 * hs} height={2 * hs} />
              ))}
          </g>
        );
      })}
      <g className="ed-preview">
        {snap.preview.map((e, i) => (
          <ElementShape key={i} e={e} />
        ))}
      </g>
      {snap.marquee && <rect className="ed-marquee" x={snap.marquee.x0} y={snap.marquee.y0} width={snap.marquee.x1 - snap.marquee.x0} height={snap.marquee.y1 - snap.marquee.y0} />}
    </svg>
  );
}
