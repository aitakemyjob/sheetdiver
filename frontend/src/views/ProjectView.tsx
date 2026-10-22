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
import { BlockDiagram, ConnectionTable, PinTables } from './ProjectConnections';

/** The project view's tabs. */
type ProjectTab = 'overview' | 'diagram' | 'pins' | 'connections';

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

/**
 * A project: rails with budgets, BOM, checks, calculations and notes on
 * the overview; the block diagram, pin tables and connection table on
 * their tabs.
 */
export function ProjectView({ params }: { params: Record<string, unknown> }) {
  const s = useStore();
  const [report, setReport] = useState(false);
  const [tab, setTab] = useState<ProjectTab>((params.tab as ProjectTab) || 'overview');
  useEffect(() => {
    if (params.tab) setTab(params.tab as ProjectTab);
  }, [params.tab, params.nonce]);
  const name = String(params.project ?? '');
  // What the agent pointed at: a rail, a BOM entry by designator or part
  // number, a net or a block.
  const highlight = String(params.highlight ?? '').toLowerCase() || undefined;
  const detail = useAsync(() => api.project(name), [name, s.projectVersion, s.libraryVersion]);
  const d = detail.data;
  useEffect(() => {
    if (d) api.uiContext({ view: tab === 'overview' ? 'project' : `project/${tab}`, project: d.project.name }).catch(() => {});
  }, [d, tab]);
  if (detail.loading && !d) return <Spinner label="Analyzing project…" />;
  if (detail.error) return <ErrorBox error={detail.error} />;
  if (!d) return null;
  const bump = () => useStore.setState({ projectVersion: useStore.getState().projectVersion + 1 });
  const checks = d.checks ?? [];
  const errors = checks.filter((c) => c.level === 'error').length;
  const warns = checks.filter((c) => c.level === 'warn').length;
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
        <div className="subtabs" style={{ marginTop: 8 }}>
          {(
            [
              ['overview', 'Overview'],
              ['diagram', 'Block diagram'],
              ['pins', 'Pins', d.connections.pinouts?.length],
              ['connections', 'Connections', d.connections.connections.length],
            ] as [ProjectTab, string, number?][]
          ).map(([k, label, n]) => (
            <button key={k} className={tab === k ? 'on' : ''} onClick={() => setTab(k)}>
              {label}
              {n ? <span className="n">{n}</span> : null}
            </button>
          ))}
        </div>
      </div>
      {tab === 'diagram' && (
        <div className="scroll pad col" style={{ flex: 1 }}>
          <BlockDiagram d={d} highlight={highlight} onChange={bump} />
        </div>
      )}
      {tab === 'pins' && (
        <div className="scroll pad col" style={{ flex: 1 }}>
          <PinTables d={d} highlight={highlight} />
        </div>
      )}
      {tab === 'connections' && (
        <div className="scroll pad col" style={{ flex: 1 }}>
          <ConnectionTable d={d} highlight={highlight} onChange={bump} />
        </div>
      )}
      <div className="scroll pad col" style={{ flex: 1, gap: 16, display: tab === 'overview' ? undefined : 'none' }}>
        <Rails d={d} onChange={bump} highlight={highlight} />
        <Bom d={d} onChange={bump} highlight={highlight} />
        {checks.length > 0 && (
          <div className="card">
            <h3>Checks against the datasheets</h3>
            <Checks checks={checks} />
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

/** The placeholder of a BOM row's current cells: what an empty cell means. */
export function currentHint(pp: ProjectPart): string {
  if (!pp.outputRail || pp.outputRail.toLowerCase() === (pp.supplyRail ?? '').toLowerCase()) return '';
  return pp.category === 'charger' ? 'charge A' : 'derived';
}

/** The tooltip of a BOM row's current cells. */
export function currentTitle(pp: ProjectPart): string {
  if (pp.category === 'charger' && pp.outputRail) return 'The charge current your board programs (e.g. with RPROG). Empty: charging is not counted.';
  if (pp.outputRail && pp.outputRail.toLowerCase() !== (pp.supplyRail ?? '').toLowerCase()) return "This regulator's input current. Empty: derived from its output rail's load.";
  return 'The current this part draws from its supply rail.';
}

/** Scrolls what the agent pointed at into view. */
function scrollTo(el: HTMLElement | null) {
  el?.scrollIntoView?.({ block: 'center' });
}

/** Reads an "other loads" entry: one current, or typ/max ("100m/150m"). */
export function parseLoad(text: string): [number | undefined, number | undefined] {
  const [a, b] = text.split('/').map((x) => (x.trim() ? parseSI(x) : undefined));
  return [a, b ?? a];
}

/** Writes typical and maximum loads as an "other loads" entry. */
export function loadText(typ?: number | null, max?: number | null): string {
  if (typ == null && max == null) return '';
  if (typ == null || max == null || typ === max) return String(typ ?? max);
  return `${typ}/${max}`;
}

/** Rail cards with load bars and an add/edit form. */
function Rails({ d, onChange, highlight }: { d: ProjectDetail; onChange: () => void; highlight?: string }) {
  const s = useStore();
  const empty = { name: '', vNom: '', vMin: '', vMax: '', limit: '', other: '' };
  const [form, setForm] = useState(empty);
  const n = (v: string) => (v.trim() ? parseSI(v) : undefined);
  const save = async () => {
    try {
      const [otherTyp, otherMax] = parseLoad(form.other);
      await api.setRail({ projectId: d.project.id, name: form.name.trim(), vNom: n(form.vNom), vMin: n(form.vMin), vMax: n(form.vMax), currentLimit: n(form.limit), otherTyp, otherMax });
      setForm(empty);
      onChange();
    } catch (e) {
      s.toast({ kind: 'err', title: 'Could not save rail', body: (e as Error).message });
    }
  };
  const edit = (r: RailStatus) =>
    setForm({
      name: r.name,
      vNom: r.vNom?.toString() ?? '',
      vMin: r.vMin?.toString() ?? '',
      vMax: r.vMax?.toString() ?? '',
      limit: r.currentLimit?.toString() ?? '',
      other: loadText(r.otherTyp, r.otherMax),
    });
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
              <div
                key={r.id}
                ref={r.name.toLowerCase() === highlight ? scrollTo : undefined}
                className={`rail-card ${r.name.toLowerCase() === highlight ? 'agent-hl' : ''}`}
                onClick={() => edit(r)}
                style={{ cursor: 'pointer' }}
                title="Click to edit"
              >
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
                  from {r.sourcePart || r.source || 'external'} · feeds {r.consumers.join(', ') || (r.otherMax || r.otherTyp ? '' : 'nothing yet')}
                  {r.otherMax || r.otherTyp ? `${r.consumers.length ? ', ' : ''}other loads ${eng(r.otherMax ?? r.otherTyp ?? undefined, 'A')}` : ''}
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
                {r.ripple != null && (
                  <div className={`faint ${r.ripple === 0 ? 'err-text' : ''}`} style={{ fontSize: 11.5 }} title={`Ripple the rail can carry before a part on it leaves its recommended input range (on top of the rail's DC tolerance): limited by ${r.rippleLimit}`}>
                    ripple budget <span className="mono">{eng(r.ripple, 'V')} p-p</span> · {r.rippleLimit}
                  </div>
                )}
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
        <input
          className="input"
          style={{ width: 150 }}
          placeholder="Other loads (A)"
          title="Loads on this rail that aren't in the BOM (a module, the sensors): one current, or typ/max such as 100m/150m"
          value={form.other}
          onChange={(e) => setForm({ ...form, other: e.target.value })}
        />
        <button className="btn" disabled={!form.name.trim()} onClick={save}>
          <Plus size={13} /> {d.rails.some((r) => r.name.toLowerCase() === form.name.trim().toLowerCase()) ? 'Update rail' : 'Add rail'}
        </button>
      </div>
    </div>
  );
}

/** The BOM, editable in place. */
function Bom({ d, onChange, highlight }: { d: ProjectDetail; onChange: () => void; highlight?: string }) {
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
                <tr
                  key={pp.id}
                  ref={highlight && (pp.designator?.toLowerCase() === highlight || pp.mpn.toLowerCase() === highlight) ? scrollTo : undefined}
                  className={highlight && (pp.designator?.toLowerCase() === highlight || pp.mpn.toLowerCase() === highlight) ? 'agent-hl' : ''}
                >
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
                    <input className="cell" placeholder={currentHint(pp)} title={currentTitle(pp)} defaultValue={pp.iTyp ?? ''} onBlur={(e) => amps(e.target.value) !== (pp.iTyp ?? null) && update(pp, { iTyp: amps(e.target.value) })} />
                  </td>
                  <td className="num">
                    <input className="cell" placeholder={currentHint(pp)} title={currentTitle(pp)} defaultValue={pp.iMax ?? ''} onBlur={(e) => amps(e.target.value) !== (pp.iMax ?? null) && update(pp, { iMax: amps(e.target.value) })} />
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
        Currents are in amps (SI prefixes work: 120m, 5µ): what each part draws from its supply rail. Leave a regulator's currents empty to derive its input current from the load on its output rail; for a charger, enter the charge current your board programs (none counts no charging).
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
