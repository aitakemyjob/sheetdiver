import { aspect, points, textAttrs, viewBox } from '../lib/cad';
import type { CadDrawing as Drawing } from '../lib/types';

/**
 * A symbol, footprint or 3D model as SheetDiver drew it: each shape
 * coloured by its role (copper, silkscreen, courtyard, body…) from the
 * theme, 3D faces in their own shaded colours.
 */
export function CadDrawing({ drawing, label, maxHeight = 320 }: { drawing: Drawing; label: string; maxHeight?: number }) {
  const ratio = aspect(drawing);
  return (
    <svg className="cad-drawing" viewBox={viewBox(drawing)} role="img" aria-label={label} style={{ width: '100%', maxHeight, aspectRatio: `${1 / Math.max(ratio, 0.05)}` }} preserveAspectRatio="xMidYMid meet">
      {drawing.items.map((s, i) => {
        const cls = `cad-${s.role}`;
        switch (s.kind) {
          case 'line': {
            const [a, b] = s.pts ?? [];
            return <line key={i} className={cls} x1={a.x} y1={a.y} x2={b.x} y2={b.y} strokeWidth={s.width} />;
          }
          case 'rect': {
            const [a, b] = s.pts ?? [];
            return <rect key={i} className={cls} x={Math.min(a.x, b.x)} y={Math.min(a.y, b.y)} width={Math.abs(b.x - a.x)} height={Math.abs(b.y - a.y)} rx={s.r} strokeWidth={s.width} />;
          }
          case 'poly':
            return <polygon key={i} className={cls} points={points(s)} style={s.color ? { fill: s.color, stroke: s.color } : undefined} />;
          case 'circle':
            return <circle key={i} className={cls} cx={s.pts?.[0].x} cy={s.pts?.[0].y} r={s.r} />;
          default:
            return (
              <text key={i} className={cls} {...textAttrs(s)}>
                {s.text}
              </text>
            );
        }
      })}
    </svg>
  );
}
