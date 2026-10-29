import { useMemo, useRef, useState } from 'react';
import { endLabels, nearestIndex, readout, scale, type ChartModel } from '../lib/simulation';

/** The chart's size in its own units; it scales to the width it's given. */
const W = 720;
const H = 260;
const M = { l: 72, r: 76, t: 22, b: 34 };

/**
 * A line chart of simulation results: one y axis, a legend for two or
 * more lines (labelled at their ends too when there are few), a
 * crosshair reading every line's value, and a table of the values.
 */
export function SimChart({ model, title, note }: { model: ChartModel; title: string; note?: string }) {
  const [hoverX, setHoverX] = useState<number | null>(null);
  const [table, setTable] = useState(false);
  const svgRef = useRef<SVGSVGElement>(null);
  const sx = useMemo(() => scale(model.x, [M.l, W - M.r], model.xLog), [model]);
  const sy = useMemo(() => scale(model.y, [H - M.b, M.t]), [model]);
  const paths = useMemo(
    () =>
      model.series.map((s) => {
        let d = '';
        for (let i = 0; i < s.x.length; i++) {
          if ((model.xLog && !(s.x[i] > 0)) || !isFinite(s.y[i])) continue;
          d += `${d ? 'L' : 'M'}${sx(s.x[i]).toFixed(1)},${sy(s.y[i]).toFixed(1)}`;
        }
        return d;
      }),
    [model, sx, sy],
  );
  const toData = (px: number) => {
    const t = (px - M.l) / (W - M.l - M.r);
    if (model.xLog) {
      const [a, b] = [Math.log10(model.x[0]), Math.log10(model.x[1])];
      return 10 ** (a + t * (b - a));
    }
    return model.x[0] + t * (model.x[1] - model.x[0]);
  };
  const onMove = (e: React.MouseEvent) => {
    const r = svgRef.current?.getBoundingClientRect();
    if (!r || r.width === 0) return;
    const px = ((e.clientX - r.left) / r.width) * W;
    setHoverX(px < M.l || px > W - M.r ? null : toData(px));
  };
  const read = hoverX === null ? null : readout(model, hoverX);
  // The lines' ends, labelled when there are few and they fit apart.
  const ends = model.series.length <= 4 ? endLabels(model.series.map((s) => (s.y.length ? sy(s.y[s.y.length - 1]) : M.t)), M.t, H - M.b) : [];
  return (
    <figure className="card sim-chart" style={{ margin: 0 }}>
      <div className="row" style={{ alignItems: 'baseline' }}>
        <h4 style={{ margin: 0 }}>{title}</h4>
        <span className="grow" />
        <button className="btn sm" aria-pressed={table} onClick={() => setTable(!table)}>
          {table ? 'Chart' : 'Table'}
        </button>
      </div>
      {model.series.length > 1 && (
        <div className="row wrap" role="list" aria-label={`${title} legend`} style={{ gap: 12, fontSize: 12, marginTop: 4 }}>
          {model.series.map((s) => (
            <span key={s.key} role="listitem" className="row" style={{ gap: 4 }}>
              <svg width="18" height="8" aria-hidden="true">
                <line x1="1" y1="4" x2="17" y2="4" stroke={s.color} strokeWidth="2" strokeDasharray={s.dash} strokeLinecap="round" />
              </svg>
              {s.label}
            </span>
          ))}
        </div>
      )}
      {table ? (
        <ValueTable model={model} />
      ) : (
        <div style={{ position: 'relative' }}>
          <svg ref={svgRef} viewBox={`0 0 ${W} ${H}`} width="100%" role="img" aria-label={title} onMouseMove={onMove} onMouseLeave={() => setHoverX(null)} style={{ display: 'block' }}>
            {model.yTicks.map((t) => (
              <g key={`y${t.v}`}>
                <line x1={M.l} x2={W - M.r} y1={sy(t.v)} y2={sy(t.v)} stroke="var(--border)" strokeWidth="1" />
                <text x={M.l - 6} y={sy(t.v)} dy="0.32em" textAnchor="end" fontSize="11" fill="var(--text-3)">
                  {t.label}
                </text>
              </g>
            ))}
            {model.xTicks.map((t) => (
              <g key={`x${t.v}`}>
                <line x1={sx(t.v)} x2={sx(t.v)} y1={M.t} y2={H - M.b} stroke="var(--border)" strokeWidth="1" opacity="0.6" />
                <text x={sx(t.v)} y={H - M.b + 14} textAnchor="middle" fontSize="11" fill="var(--text-3)">
                  {t.label}
                </text>
              </g>
            ))}
            <text x={(M.l + W - M.r) / 2} y={H - 4} textAnchor="middle" fontSize="11" fill="var(--text-3)">
              {model.xLabel}
              {model.xLog ? ' (log)' : ''}
            </text>
            <text x={M.l - 6} y={12} textAnchor="end" fontSize="11" fill="var(--text-3)">
              {model.yLabel}
            </text>
            {paths.map((d, i) => (
              <path key={model.series[i].key} d={d} fill="none" stroke={model.series[i].color} strokeWidth="2" strokeDasharray={model.series[i].dash} strokeLinejoin="round" strokeLinecap="round" />
            ))}
            {model.series.map((s, i) =>
              ends[i] == null ? null : (
                <text key={`end${s.key}`} x={W - M.r + 6} y={ends[i]!} dy="0.32em" fontSize="11" fill="var(--text)">
                  {s.short.length > 12 ? s.short.slice(0, 11) + '…' : s.short}
                </text>
              ),
            )}
            {hoverX !== null && (
              <g pointerEvents="none">
                <line x1={sx(hoverX)} x2={sx(hoverX)} y1={M.t} y2={H - M.b} stroke="var(--text-3)" strokeWidth="1" />
                {model.series.map((s) => {
                  const i = nearestIndex(s.x, hoverX);
                  return i < 0 ? null : <circle key={`dot${s.key}`} cx={sx(s.x[i])} cy={sy(s.y[i])} r="4" fill={s.color} stroke="var(--bg)" strokeWidth="2" />;
                })}
              </g>
            )}
          </svg>
          {read && (
            <div className="sim-tooltip" role="status" style={{ position: 'absolute', top: 8, left: `${(Math.min(sx(hoverX!), W - 220) / W) * 100}%`, pointerEvents: 'none' }}>
              <div style={{ fontWeight: 600 }}>{read.x}</div>
              {read.rows.map((r) => (
                <div key={r.label} className="row" style={{ gap: 6 }}>
                  <span style={{ width: 8, height: 8, borderRadius: 4, background: r.color, display: 'inline-block' }} />
                  <span className="grow">{r.label}</span>
                  <b>{r.value}</b>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
      {note && <figcaption className="muted" style={{ fontSize: 12, marginTop: 4 }}>{note}</figcaption>}
    </figure>
  );
}

/** The chart's values as a table: about twenty points along the x axis, each line's value there. */
function ValueTable({ model }: { model: ChartModel }) {
  const xs = model.series[0]?.x ?? [];
  const step = Math.max(1, Math.floor(xs.length / 20));
  const rows = xs.filter((_, i) => i % step === 0);
  return (
    <div className="scroll" style={{ maxHeight: 260 }}>
      <table className="dtable" aria-label="Chart values">
        <thead>
          <tr>
            <th>{model.xLabel}</th>
            {model.series.map((s) => (
              <th key={s.key}>{s.label}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((x) => {
            const r = readout(model, x);
            return (
              <tr key={x}>
                <td>{r.x}</td>
                {r.rows.map((c) => (
                  <td key={c.label}>{c.value}</td>
                ))}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
