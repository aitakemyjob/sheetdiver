import { useEffect, useState } from 'react';
import { AlertOctagon, AlertTriangle, Calculator, CheckCircle2, FileText, Plus, Trash2, Zap } from 'lucide-react';
import { api } from '../lib/api';
import { categories, eng, parseSI, relTime } from '../lib/format';
import { useStore } from '../lib/store';
import { StatusBadge } from '../components/PartStatus';
import { ReportDialog } from '../components/ReportDialog';
import type { Check, ProjectDetail, ProjectPart, RailStatus } from '../lib/types';
import { CategoryBadge, Empty, ErrorBox, Spinner, useAsync } from '../components/ui';
import { NotesPanel } from './NotesPanel';
import { calcFor } from './PartView';

/** Icon for a check level. */
export function CheckIcon({ level }: { level: string }) {
  if (level === 'error') return <AlertOctagon size={14} style={{ color: 'var(--err)' }} />;
  if (level === 'warn') return <AlertTriangle size={14} style={{ color: 'var(--warn)' }} />;
  return <CheckCircle2 size={14} style={{ color: 'var(--ok)' }} />;
}

/** A list of checks. */
export function Checks({ checks }: { checks: Check[] }) {
  return (
    <div className="col" style={{ gap: 4 }}>
      {checks.map((c, i) => (
        <div key={i} className={`check ${c.level}`}>
          <CheckIcon level={c.level} />
          <span>{c.message}</span>
        </div>
      ))}
    </div>
  );
}

/** A project: rails with budgets, BOM, checks, calculations and notes. */
export function ProjectView({ params }: { params: Record<string, unknown> }) {
  const s = useStore();
  const [report, setReport] = useState(false);
  const name = String(params.project ?? '');
  const detail = useAsync(() => api.project(name), [name, s.projectVersion, s.libraryVersion]);
  const d = detail.data;
  useEffect(() => {
    if (d) api.uiContext({ view: 'project', project: d.project.name }).catch(() => {});
  }, [d]);
  if (detail.loading && !d) return <Spinner label="Analyzing project…" />;
  if (detail.error) return <ErrorBox error={detail.error} />;
  if (!d) return null;
  const bump = () => useStore.setState({ projectVersion: useStore.getState().projectVersion + 1 });
  const errors = d.checks.filter((c) => c.level === 'error').length;
  const warns = d.checks.filter((c) => c.level === 'warn').length;
  return (
    <div className="view">
      <div className="part-head" style={{ paddingBottom: 12 }}>
        <h1 style={{ fontFamily: 'var(--font-ui)' }}>
          {d.project.name}
          {errors > 0 && <span className="badge err">{errors} error{errors === 1 ? '' : 's'}</span>}
          {warns > 0 && <span className="badge warn">{warns} warning{warns === 1 ? '' : 's'}</span>}
          {errors + warns === 0 && d.parts.length > 0 && <span className="badge ok">checks pass</span>}
          <span className="grow" />
          <button className="btn sm" title="Design-review report (Markdown): findings, rails, parts, calculations" onClick={() => setReport(true)}>
            <FileText size={13} /> Report
          </button>
        </h1>
        <input
          className="input"
          style={{ maxWidth: 700 }}
          defaultValue={d.project.description}
          placeholder="Description"
          onBlur={(e) => e.target.value !== d.project.description && api.updateProject({ ...d.project, description: e.target.value }).then(bump)}
        />
        <div className="stats" style={{ marginTop: 8 }}>
          <div className="stat">
            <b>{d.parts.length}</b>
            <span>BOM lines</span>
          </div>
          <div className="stat">
            <b>{d.rails.length}</b>
            <span>Rails</span>
          </div>
          <div className="stat">
            <b>{eng(d.inputPower, 'W')}</b>
            <span>Input power (max)</span>
          </div>
          <div className="stat">
            <b>{d.calculations.length}</b>
            <span>Calculations</span>
          </div>
        </div>
      </div>
      <div className="scroll pad col" style={{ flex: 1, gap: 16 }}>
        <Rails d={d} onChange={bump} />
        <Bom d={d} onChange={bump} />
        {d.checks.length > 0 && (
          <div className="card">
            <h3>Checks against the datasheets</h3>
            <Checks checks={d.checks} />
          </div>
        )}
        <div className="card">
          <h3>Saved calculations</h3>
          {d.calculations.length === 0 && <div className="muted">Run a calculator with this project selected and save it to keep it here.</div>}
          {d.calculations.map((c) => (
            <div key={c.id} className="row" style={{ padding: '6px 0', borderBottom: '1px solid var(--border)' }}>
              <Calculator size={14} className="faint" />
              <div className="grow">
                <div>
                  <b>{c.title}</b> <span className="faint">{relTime(c.createdAt)}</span>
                </div>
                <div className="muted" style={{ fontSize: 12 }}>{c.outputs?.result?.summary}</div>
              </div>
              <button className="btn sm" onClick={() => s.open({ kind: 'calc', title: c.title, params: { kind: c.kind, part: c.outputs?.part, project: d.project.name, result: c.outputs, inputs: c.inputs } })}>
                Open
              </button>
              <button
                className="btn ghost icon sm"
                onClick={async () => {
                  await api.deleteCalculation(c.id);
                  bump();
                }}
              >
                <Trash2 size={12} />
              </button>
            </div>
          ))}
        </div>
        <div className="card">
          <h3>Notes</h3>
          <NotesPanel kind="project" id={d.project.name} />
        </div>
      </div>
      {report && <ReportDialog project={d.project.name} onClose={() => setReport(false)} />}
    </div>
  );
}

/** Rail cards with load bars and an add/edit form. */
function Rails({ d, onChange }: { d: ProjectDetail; onChange: () => void }) {
  const s = useStore();
  const [form, setForm] = useState({ name: '', vNom: '', vMin: '', vMax: '', limit: '' });
  const n = (v: string) => (v.trim() ? parseSI(v) : undefined);
  const save = async () => {
    try {
      await api.setRail({ projectId: d.project.id, name: form.name.trim(), vNom: n(form.vNom), vMin: n(form.vMin), vMax: n(form.vMax), currentLimit: n(form.limit) });
      setForm({ name: '', vNom: '', vMin: '', vMax: '', limit: '' });
      onChange();
    } catch (e) {
      s.toast({ kind: 'err', title: 'Could not save rail', body: (e as Error).message });
    }
  };
  const edit = (r: RailStatus) =>
    setForm({ name: r.name, vNom: r.vNom?.toString() ?? '', vMin: r.vMin?.toString() ?? '', vMax: r.vMax?.toString() ?? '', limit: r.currentLimit?.toString() ?? '' });
  return (
    <div className="card col">
      <h3>
        <Zap size={13} /> Rails
      </h3>
      {d.rails.length > 0 && (
        <div className="grid2">
          {d.rails.map((r) => {
            const u = r.capacity ? r.loadMax / r.capacity : 0;
            return (
              <div key={r.id} className="rail-card" onClick={() => edit(r)} style={{ cursor: 'pointer' }} title="Click to edit">
                <div className="row">
                  <span className="name">{r.name}</span>
                  <span className="volt">{eng(r.vNom ?? undefined, 'V')}</span>
                  <span className="faint mono">
                    {r.vMin != null || r.vMax != null ? `${eng(r.vMin ?? r.vNom ?? undefined, 'V')} – ${eng(r.vMax ?? r.vNom ?? undefined, 'V')}` : ''}
                  </span>
                  <div className="grow" />
                  <button
                    className="btn ghost icon sm"
                    onClick={async (e) => {
                      e.stopPropagation();
                      await api.deleteRail(d.project.id, r.name);
                      onChange();
                    }}
                  >
                    <Trash2 size={12} />
                  </button>
                </div>
                <div className="faint" style={{ fontSize: 11.5 }}>
                  from {r.sourcePart || r.source || 'external'} · feeds {r.consumers.join(', ') || 'nothing yet'}
                </div>
                <div className="row mono" style={{ fontSize: 12 }}>
                  load {eng(r.loadTyp, 'A')} typ / {eng(r.loadMax, 'A')} max
                  {r.capacity ? <span className="faint"> of {eng(r.capacity, 'A')}</span> : null}
                  {!r.capacity && r.ceiling ? (
                    <span className="faint" title={`${r.ceilingFrom}: an upper bound, not a rating`}>
                      {' '}
                      · capacity unknown (limit {eng(r.ceiling, 'A')})
                    </span>
                  ) : null}
                  <span className="grow" />
                  {eng(r.power, 'W')}
                </div>
                {r.capacity ? (
                  <div className={`bar ${u > 1 ? 'err' : u > 0.8 ? 'warn' : ''}`} title={r.capacityFrom}>
                    <i style={{ width: `${Math.min(100, u * 100)}%` }} />
                  </div>
                ) : null}
              </div>
            );
          })}
        </div>
      )}
      <div className="row" style={{ flexWrap: 'wrap' }}>
        <input className="input" style={{ width: 110 }} placeholder="Rail (3V3)" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
        <input className="input" style={{ width: 90 }} placeholder="V nom" value={form.vNom} onChange={(e) => setForm({ ...form, vNom: e.target.value })} />
        <input className="input" style={{ width: 90 }} placeholder="V min" value={form.vMin} onChange={(e) => setForm({ ...form, vMin: e.target.value })} />
        <input className="input" style={{ width: 90 }} placeholder="V max" value={form.vMax} onChange={(e) => setForm({ ...form, vMax: e.target.value })} />
        <input className="input" style={{ width: 110 }} placeholder="Limit (A)" value={form.limit} onChange={(e) => setForm({ ...form, limit: e.target.value })} />
        <button className="btn" disabled={!form.name.trim()} onClick={save}>
          <Plus size={13} /> {d.rails.some((r) => r.name.toLowerCase() === form.name.trim().toLowerCase()) ? 'Update rail' : 'Add rail'}
        </button>
      </div>
    </div>
  );
}

/** The BOM, editable in place. */
function Bom({ d, onChange }: { d: ProjectDetail; onChange: () => void }) {
  const s = useStore();
  const [part, setPart] = useState('');
  const matches = useAsync(() => (part.trim().length >= 2 ? api.parts({ query: part.trim(), limit: 8 }) : Promise.resolve([])), [part]);
  const rails = d.rails.map((r) => r.name);
  const update = async (pp: ProjectPart, patch: Partial<ProjectPart>) => {
    try {
      await api.updateProjectPart({ ...pp, ...patch });
      onChange();
    } catch (e) {
      s.toast({ kind: 'err', title: 'Could not update', body: (e as Error).message });
    }
  };
  const add = async (mpn: string) => {
    try {
      await api.addProjectPart(d.project.name, mpn, { qty: 1 });
      setPart('');
      onChange();
    } catch (e) {
      s.toast({ kind: 'err', title: 'Could not add part', body: (e as Error).message });
    }
  };
  const railSelect = (pp: ProjectPart, field: 'supplyRail' | 'outputRail') => (
    <select className="input select" style={{ height: 24, fontSize: 12 }} value={pp[field] ?? ''} onChange={(e) => update(pp, { [field]: e.target.value })}>
      <option value="">—</option>
      {rails.map((r) => (
        <option key={r}>{r}</option>
      ))}
      {pp[field] && !rails.includes(pp[field]!) && <option>{pp[field]}</option>}
    </select>
  );
  const amps = (v: string) => (v.trim() === '' ? null : (parseSI(v) ?? null));
  return (
    <div className="card col">
      <h3>BOM</h3>
      {d.parts.length === 0 ? (
        <Empty icon={<Plus size={24} />} title="No parts yet">
          Add parts below, import a BOM (Import → BOM), use a part's “Add to project” button, or ask your agent to build it.
        </Empty>
      ) : (
        <table className="dtable">
          <thead>
            <tr>
              <th style={{ width: 70 }}>Ref</th>
              <th>Part</th>
              <th style={{ width: 46 }}>Qty</th>
              <th>Role</th>
              <th style={{ width: 100 }}>Supply</th>
              <th style={{ width: 100 }}>Output</th>
              <th style={{ width: 80, textAlign: 'right' }}>I typ</th>
              <th style={{ width: 80, textAlign: 'right' }}>I max</th>
              <th style={{ width: 70 }} />
            </tr>
          </thead>
          <tbody>
            {d.parts.map((pp) => {
              const calc = calcFor(pp.category);
              return (
                <tr key={pp.id}>
                  <td>
                    <input className="cell" defaultValue={pp.designator} onBlur={(e) => e.target.value !== (pp.designator ?? '') && update(pp, { designator: e.target.value })} />
                  </td>
                  <td className="mono">
                    <a style={{ cursor: 'pointer' }} onClick={() => s.open({ kind: 'part', title: pp.mpn, params: { part: pp.mpn } })}>
                      {pp.mpn}
                    </a>{' '}
                    {pp.variant && <span className="faint">({pp.variant})</span>} <CategoryBadge category={pp.category} label={categories[pp.category]} /> <StatusBadge status={pp.status} />
                    {!pp.hasDatasheet && (
                      <span className="badge warn" title="Not imported yet, so its limits aren't checked">
                        no datasheet
                      </span>
                    )}
                  </td>
                  <td className="num">
                    <input className="cell" defaultValue={pp.qty} onBlur={(e) => Number(e.target.value) !== pp.qty && update(pp, { qty: Number(e.target.value) || 1 })} />
                  </td>
                  <td>
                    <input className="cell" defaultValue={pp.role} onBlur={(e) => e.target.value !== (pp.role ?? '') && update(pp, { role: e.target.value })} />
                  </td>
                  <td>{railSelect(pp, 'supplyRail')}</td>
                  <td>{railSelect(pp, 'outputRail')}</td>
                  <td className="num">
                    <input className="cell" placeholder={pp.outputRail ? 'derived' : ''} defaultValue={pp.iTyp ?? ''} onBlur={(e) => amps(e.target.value) !== (pp.iTyp ?? null) && update(pp, { iTyp: amps(e.target.value) })} />
                  </td>
                  <td className="num">
                    <input className="cell" placeholder={pp.outputRail ? 'derived' : ''} defaultValue={pp.iMax ?? ''} onBlur={(e) => amps(e.target.value) !== (pp.iMax ?? null) && update(pp, { iMax: amps(e.target.value) })} />
                  </td>
                  <td>
                    <div className="row" style={{ gap: 2 }}>
                      {calc && (
                        <button className="btn ghost icon sm" title={`Run the ${calc} calculator with this part's rails`} onClick={() => s.open({ kind: 'calc', title: `${calc} ${pp.mpn}`, params: { kind: calc, part: pp.mpn, project: d.project.name } })}>
                          <Calculator size={12} />
                        </button>
                      )}
                      <button
                        className="btn ghost icon sm"
                        title="Remove from project"
                        onClick={async () => {
                          await api.removeProjectPart(d.project.id, pp.id);
                          onChange();
                        }}
                      >
                        <Trash2 size={12} />
                      </button>
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
      <div className="muted" style={{ fontSize: 11.5 }}>
        Currents are in amps (SI prefixes work: 120m, 5µ). Leave a regulator's currents empty to derive its input current from the load on its output rail.
      </div>
      <div className="row" style={{ position: 'relative' }}>
        <input className="input" style={{ width: 320 }} placeholder="Add a part: type a part number" value={part} onChange={(e) => setPart(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && part.trim() && add(part.trim())} />
        {(matches.data ?? []).map((p) => (
          <button key={p.id} className="chip" onClick={() => add(p.mpn)}>
            {p.mpn}
          </button>
        ))}
      </div>
    </div>
  );
}
