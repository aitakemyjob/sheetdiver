import { useEffect, useMemo, useState } from 'react';
import { Save } from 'lucide-react';
import { api } from '../lib/api';
import { eng, parseSI } from '../lib/format';
import { useStore } from '../lib/store';
import type { CalcResponse } from '../lib/types';
import { ErrorBox, useAsync } from '../components/ui';
import { Checks } from './ProjectView';

const kinds: [string, string][] = [
  ['buck', 'Buck'],
  ['boost', 'Boost'],
  ['ldo', 'LDO'],
  ['gate_drive', 'Gate drive'],
  ['gate_divider', 'Gate divider'],
  ['divider', 'Divider'],
];

/** Units of calculator inputs, by field name, for labels and parsing. */
function unitOf(field: string, description: string): string {
  const m = description.match(/\(([^)]*)\)\s*(?:,|$|;| default| e\.g\.)/);
  if (m && /^(V|A|Hz|s|H|F|Ω|C|°C|°C\/W)$/.test(m[1])) return m[1];
  if (/^(rippleRatio|efficiency|margin|dmax)$/.test(field)) return '';
  return '';
}

/**
 * A calculator: inputs (filled from the part's datasheet and project rails
 * when given), outputs with formulas, checks and suggestions.
 */
export function CalcView({ params }: { params: Record<string, unknown> }) {
  const s = useStore();
  const [kind, setKind] = useState(String(params.kind ?? 'buck'));
  const [part, setPart] = useState(String(params.part ?? ''));
  const [project, setProject] = useState(String(params.project ?? ''));
  const [text, setText] = useState<Record<string, string>>(() => {
    const init: Record<string, string> = {};
    const inputs = (params.inputs ?? (params.result as CalcResponse | undefined)?.inputs ?? {}) as Record<string, unknown>;
    for (const [k, v] of Object.entries(inputs)) init[k] = String(v);
    return init;
  });
  const [result, setResult] = useState<CalcResponse | undefined>(params.result as CalcResponse | undefined);
  const [error, setError] = useState<unknown>();
  const fields = useAsync(() => api.calcFields(kind), [kind]);
  const projects = useAsync(() => api.projects(), [s.projectVersion]);

  const inputs = useMemo(() => {
    const out: Record<string, unknown> = {};
    for (const f of fields.data ?? []) {
      const v = (text[f.name] ?? '').trim();
      if (!v) continue;
      if (f.type === 'string') out[f.name] = v;
      else {
        const n = parseSI(v);
        if (n !== undefined) out[f.name] = n;
      }
    }
    return out;
  }, [text, fields.data]);

  useEffect(() => {
    if (!fields.data) return;
    const t = setTimeout(async () => {
      try {
        setResult(await api.calc({ kind, part: part.trim() || undefined, project: project || undefined, inputs }));
        setError(undefined);
      } catch (e) {
        setError(e);
      }
    }, 250);
    return () => clearTimeout(t);
  }, [kind, part, project, inputs, fields.data]);

  const filled = new Map((result?.filled ?? []).map((f) => [f.field, f]));
  const save = async () => {
    try {
      const r = await api.calc({ kind, part: part.trim() || undefined, project: project || undefined, inputs, save: true });
      s.toast({ kind: 'ok', title: `Saved calculation #${r.savedId}`, body: project ? `in ${project}` : undefined });
    } catch (e) {
      s.toast({ kind: 'err', title: 'Could not save', body: (e as Error).message });
    }
  };
  const res = result?.result;
  return (
    <div className="view">
      <div className="toolbar">
        <div className="seg">
          {kinds.map(([k, label]) => (
            <button
              key={k}
              className={k === kind ? 'on' : ''}
              onClick={() => {
                setKind(k);
                setResult(undefined);
              }}
            >
              {label}
            </button>
          ))}
        </div>
        <input className="input" style={{ width: 200 }} placeholder="Part (fills datasheet values)" value={part} onChange={(e) => setPart(e.target.value)} />
        <select className="input select" style={{ width: 180 }} value={project} onChange={(e) => setProject(e.target.value)}>
          <option value="">No project</option>
          {projects.data?.map((p) => (
            <option key={p.id}>{p.name}</option>
          ))}
        </select>
        <div className="grow" />
        <button className="btn primary sm" disabled={!res} onClick={save}>
          <Save size={13} /> Save{project ? ` to ${project}` : ''}
        </button>
      </div>
      <div className="scroll" style={{ flex: 1 }}>
        <div className="calc">
          <div className="card col" style={{ gap: 10 }}>
            <h3>Inputs</h3>
            <div className="muted" style={{ fontSize: 11.5 }}>
              SI units; prefixes work (4.7u, 650k, 2m). Leave a field empty to take it from the datasheet or project; filled values are shown in the placeholder.
            </div>
            {fields.data?.map((f) => {
              const fl = filled.get(f.name);
              const unit = unitOf(f.name, f.description);
              return (
                <div key={f.name} className={`field ${fl ? 'filled' : ''}`}>
                  <label>
                    <span className="mono">{f.name}</span>
                    {f.required === 'true' && <span style={{ color: 'var(--warn)' }}>*</span>}
                    {unit && <span className="unit">{unit}</span>}
                  </label>
                  <input
                    className="input"
                    value={text[f.name] ?? ''}
                    placeholder={fl ? `${eng(fl.value, fl.unit)} (${fl.from.startsWith('project') ? 'project' : 'datasheet'})` : ''}
                    onChange={(e) => setText({ ...text, [f.name]: e.target.value })}
                  />
                  <span className="help">{fl ? fl.from : f.description}</span>
                </div>
              );
            })}
          </div>
          <div className="col" style={{ gap: 14 }}>
            <ErrorBox error={error} />
            {res && (
              <>
                <div className="card">
                  <h3>Result</h3>
                  <div style={{ fontSize: 14, fontWeight: 600 }}>{res.summary}</div>
                </div>
                {(res.checks?.length ?? 0) > 0 && (
                  <div className="card">
                    <h3>Checks</h3>
                    <Checks checks={res.checks!} />
                  </div>
                )}
                <div className="outputs">
                  {res.outputs?.map((o) => (
                    <div key={o.name} className="out" title={o.formula}>
                      <div className="n">
                        {o.name} {o.symbol && <span className="mono faint">{o.symbol}</span>}
                      </div>
                      <div className="v">{eng(o.value, o.unit)}</div>
                      {o.formula && <div className="f">{o.formula}</div>}
                    </div>
                  ))}
                </div>
                {(res.suggestions?.length ?? 0) > 0 && (
                  <div className="card">
                    <h3>Suggestions</h3>
                    <ul style={{ margin: 0, paddingLeft: 18, lineHeight: 1.7 }}>
                      {res.suggestions!.map((x, i) => (
                        <li key={i}>{x}</li>
                      ))}
                    </ul>
                  </div>
                )}
                {(result?.filled.length ?? 0) > 0 && (
                  <div className="card">
                    <h3>From the datasheet and project</h3>
                    {result!.filled.map((f) => (
                      <div key={f.field} className="filled-from">
                        <b>{f.field}</b> = {eng(f.value, f.unit)} — {f.from}
                      </div>
                    ))}
                  </div>
                )}
                {(result?.missing?.length ?? 0) > 0 && (
                  <div className="card">
                    <h3>Not found on the datasheet</h3>
                    <div className="muted">
                      {result!.missing!.join(', ')}. Checks that need them were skipped; enter them above or record them on the part's Parameters tab.
                    </div>
                  </div>
                )}
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
