import { arcPoints } from '../../lib/editor/geometry';
import { pinEnd, type Element } from '../../lib/editor/elements';
import type { Graphic } from '../../lib/editor/model';

/** The colour class of a footprint layer or a symbol's body. */
const roleOf = (layer: string) => `cad-${layer === 'body' ? 'body' : layer}`;

/** A drawn graphic. */
function GraphicShape({ g }: { g: Graphic }) {
  const cls = `${roleOf(g.layer)} ed-graphic${g.fill ? ' filled' : ''}`;
  const w = g.width || 0.05;
  switch (g.kind) {
    case 'line':
      return <line className={cls} x1={g.pts[0].x} y1={g.pts[0].y} x2={g.pts[1].x} y2={g.pts[1].y} strokeWidth={w} />;
    case 'rect': {
      const [a, b] = g.pts;
      return <rect className={cls} x={Math.min(a.x, b.x)} y={Math.min(a.y, b.y)} width={Math.abs(b.x - a.x)} height={Math.abs(b.y - a.y)} strokeWidth={w} />;
    }
    case 'circle':
      return <circle className={cls} cx={g.pts[0].x} cy={g.pts[0].y} r={g.r} strokeWidth={w} />;
    case 'arc':
      return <polyline className={cls} points={arcPoints(g.pts[0], g.pts[1], g.pts[2]).map((p) => `${p.x},${p.y}`).join(' ')} strokeWidth={w} />;
    case 'poly':
      return <polygon className={cls} points={g.pts.map((p) => `${p.x},${p.y}`).join(' ')} strokeWidth={w} />;
    case 'text':
      return (
        <text className={`${roleOf(g.layer)} ed-text`} x={g.pts[0].x} y={g.pts[0].y} fontSize={g.size} transform={g.angle ? `rotate(${-g.angle} ${g.pts[0].x} ${g.pts[0].y})` : undefined}>
          {g.text}
        </text>
      );
  }
}

/** An element of the drawing as SVG. */
export function ElementShape({ e }: { e: Element }) {
  switch (e.type) {
    case 'graphic':
      return <GraphicShape g={e.g} />;
    case 'pad': {
      const p = e.pad;
      const r = p.shape === 'circle' || p.shape === 'oval' ? Math.min(p.w, p.h) / 2 : p.shape === 'roundrect' ? 0.25 * Math.min(p.w, p.h) : 0;
      const size = Math.min(0.8, 0.6 * Math.min(p.w, p.h));
      return (
        <g>
          {p.kind !== 'npth' && <rect className={p.kind === 'paste' ? 'cad-paste' : 'cad-copper'} x={p.x - p.w / 2} y={p.y - p.h / 2} width={p.w} height={p.h} rx={r} />}
          {!!p.drill && <circle className="cad-drill ed-drill" cx={p.x} cy={p.y} r={p.drill / 2} />}
          {p.kind !== 'paste' && p.number && (
            <text className="ed-padnum" x={p.x} y={p.y + size * 0.35} fontSize={size} textAnchor="middle">
              {p.number}
            </text>
          )}
        </g>
      );
    }
    case 'pin': {
      const p = e.pin;
      const end = pinEnd(p);
      const vertical = p.side === 'top' || p.side === 'bottom';
      const mid = { x: (p.x + end.x) / 2, y: (p.y + end.y) / 2 };
      const anchor = p.side === 'left' || p.side === 'bottom' ? 'start' : 'end';
      const inset = { left: [0.6, 0.45], right: [-0.6, 0.45], top: [0.45, 0.6], bottom: [0.45, -0.6] }[p.side];
      const nameAt = { x: end.x + inset[0], y: end.y + inset[1] };
      return (
        <g opacity={p.hidden ? 0.4 : 1}>
          <line className="cad-pin" x1={p.x} y1={p.y} x2={end.x} y2={end.y} strokeWidth={0.2} />
          <circle className="ed-pin-end" cx={p.x} cy={p.y} r={0.25} />
          <text className="cad-pinNumber" x={vertical ? mid.x - 0.35 : mid.x} y={vertical ? mid.y : mid.y - 0.35} fontSize={1} textAnchor="middle" transform={vertical ? `rotate(-90 ${mid.x - 0.35} ${mid.y})` : undefined}>
            {p.number}
          </text>
          {p.name !== '~' && (
            <text className="cad-pinName" x={nameAt.x} y={nameAt.y} fontSize={1.27} textAnchor={anchor} transform={vertical ? `rotate(-90 ${nameAt.x} ${nameAt.y})` : undefined}>
              {p.name}
            </text>
          )}
        </g>
      );
    }
    case 'body':
      return <rect className="cad-body" x={e.box.x0} y={e.box.y0} width={e.box.x1 - e.box.x0} height={e.box.y1 - e.box.y0} strokeWidth={0.254} />;
  }
}
