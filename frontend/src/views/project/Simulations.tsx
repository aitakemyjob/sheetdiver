import { useEffect, useState } from 'react';
import { AlertTriangle, CheckCircle2, FlaskConical, Play, Trash2, XCircle } from 'lucide-react';
import { api } from '../../lib/api';
import { relTime } from '../../lib/format';
import { axisName, chartModel, conditionsOf, defaultVariations, formatValue, noteOf, panels, runsFor, stateBadge, targetMatrix, type ChartModel, type Panel } from '../../lib/simulation';
import { darkTheme } from '../../lib/diagramPalette';
import { useStore } from '../../lib/store';
import type { NgspiceState, SimRun, SimulationDetail, SimulationSummary, VariationDetail } from '../../lib/types';
import { Empty, ErrorBox, Markdown, Spinner, useAsync } from '../../components/ui';
import { SimChart } from '../../components/SimChart';

/**
 * A project's simulations: its tests, each one question the agent set
 * out to answer, and the one picked with how it fared across its
 * variations — the write-up, the targets for every variation in every
 * condition, the charts comparing them, and each variation in turn.
 */
export function Simulations({ project, highlight }: { project: string; highlight?: string }) {
  const s = useStore();
  const list = useAsync(() => api.simulations(project), [project, s.projectVersion]);
  const ngspice = useAsync(() => api.ngspice(), [s.app?.config.settings.ngspicePath]);
  const [picked, setPicked] = useState<string | undefined>(undefined);
  const tests = list.data ?? [];
  useEffect(() => {
    if (highlight) setPicked(tests.find((x) => x.name.toLowerCase() === highlight)?.name ?? picked);
  }, [highlight, list.data]); // eslint-disable-line react-hooks/exhaustive-deps
  if (list.loading && !list.data) return <Spinner label="Loading simulations…" />;
  if (list.error) return <ErrorBox error={list.error} />;
  if (tests.length === 0)
    return (
      <Empty icon={<FlaskConical size={28} />} title="No simulations yet">
        Ask your agent to test this board — start-up, inrush, a load step, loop gain, corners. It sets up each test, tries it in variations as the schematic changes, and runs them here with ngspice.
        <NgspiceLine state={ngspice.data} />
      </Empty>
    );
  const name = picked && tests.some((t) => t.name === picked) ? picked : tests[0].name;
  return (
    <div className="row" style={{ alignItems: 'flex-start', gap: 16 }}>
      <nav aria-label="Simulations" className="col" style={{ width: 260, flexShrink: 0, gap: 6 }}>
        {tests.map((t) => (
          <TestItem key={t.name} test={t} on={t.name === name} onPick={() => setPicked(t.name)} />
        ))}
        <NgspiceLine state={ngspice.data} />
      </nav>
      <div className="grow" style={{ minWidth: 0 }}>
        <TestPanel project={project} name={name} version={s.projectVersion} canRun={!!ngspice.data?.version} onDeleted={() => setPicked(undefined)} />
      </div>
    </div>
  );
}

/** A test in the list: name, kind, how many variations, and how the latest fares. */
function TestItem({ test, on, onPick }: { test: SimulationSummary; on: boolean; onPick: () => void }) {
  const failed = Object.values(test.states).filter((st) => st === 'failed').length;
  return (
    <button className={`card sim-item${on ? ' on' : ''}`} aria-current={on} onClick={onPick} style={{ textAlign: 'left', padding: 10 }}>
      <div style={{ fontWeight: 600 }}>{test.name}</div>
      {test.kind && <div className="muted" style={{ fontSize: 12 }}>{test.kind}</div>}
      <div className="muted" style={{ fontSize: 12, marginTop: 2 }}>
        {test.variations} variation{test.variations === 1 ? '' : 's'}
        {test.latest ? ` · latest ${test.latest}` : ''}
      </div>
      <div className="row wrap" style={{ gap: 6, marginTop: 4, fontSize: 12 }}>
        {failed > 0 && <span className="badge err">{failed} failed</span>}
        {test.pass > 0 && <span className="badge ok">{test.pass} pass</span>}
        {test.fail > 0 && <span className="badge err">{test.fail} fail</span>}
        {!test.presented && test.variations > 0 && <span className="badge">not written up</span>}
      </div>
    </button>
  );
}

/** Which ngspice runs the simulations, or how to get one. */
function NgspiceLine({ state }: { state?: NgspiceState }) {
  if (!state) return null;
  if (state.version)
    return (
      <div className="faint" style={{ fontSize: 12 }}>
        ngspice {state.version} · {state.path}
      </div>
    );
  return (
    <div className="muted" style={{ fontSize: 12 }}>
      <AlertTriangle size={12} /> {state.error} (Settings → Simulation)
    </div>
  );
}

/** A test: what it asks, the agent's write-up, the targets across variations, the charts comparing them, and its variations. */
function TestPanel({ project, name, version, canRun, onDeleted }: { project: string; name: string; version: number; canRun: boolean; onDeleted: () => void }) {
  const detail = useAsync(() => api.simulation(project, name), [project, name, version]);
  const d = detail.data;
  if (detail.loading && !d) return <Spinner label="Loading…" />;
  if (detail.error) return <ErrorBox error={detail.error} />;
  if (!d) return null;
  const sim = d.simulation;
  const p = sim.presentation;
  return (
    <article className="col" style={{ gap: 12 }} aria-label={`Test ${sim.name}`}>
      <header className="col" style={{ gap: 4 }}>
        <h2 className="row" style={{ margin: 0, gap: 8 }}>
          {sim.name}
          {sim.kind && <span className="badge">{sim.kind}</span>}
          <span className="grow" />
          <button
            className="btn sm danger"
            onClick={async () => {
              if (!window.confirm(`Delete the test ${sim.name}, all its variations and their results?`)) return;
              await api.deleteSimulation(project, name);
              onDeleted();
            }}
          >
            <Trash2 size={13} /> Delete test
          </button>
        </h2>
        {sim.purpose && <div>{sim.purpose}</div>}
        <div className="faint" style={{ fontSize: 12 }}>
          {sim.target ? `Tests ${sim.target} on its own` : 'The board'} · {d.variations.length} variation{d.variations.length === 1 ? '' : 's'} · set up by {sim.source} {relTime(sim.created)}
        </div>
      </header>
      {p ? (
        <section className="card" aria-label="Write-up">
          <Markdown src={p.summary} />
          {(p.standIns?.length ?? 0) > 0 && (
            <div className="muted" style={{ fontSize: 12 }}>
              <AlertTriangle size={12} /> Stand-ins, not real models: {p.standIns!.join('; ')}
            </div>
          )}
        </section>
      ) : (
        <div className="muted">The agent hasn’t written up this test yet.</div>
      )}
      <Targets d={d} />
      {p && <Charts d={d} />}
      {p?.conclusions && (
        <section className="card" aria-label="Conclusions">
          <h4 style={{ marginTop: 0 }}>Conclusions</h4>
          <Markdown src={p.conclusions} />
        </section>
      )}
      <section className="col" style={{ gap: 8 }} aria-label="Variations">
        <h4 style={{ margin: '4px 0 0' }}>Variations, latest first</h4>
        {[...d.variations].reverse().map((v) => (
          <VariationCard key={v.variation.id} project={project} test={name} v={v} n={d.variations.indexOf(v) + 1} note={noteOf(d, v.variation.name)} canRun={canRun} />
        ))}
      </section>
    </article>
  );
}

/** The targets, one matrix per measurement: a row per variation, in the order they were added, a cell per condition, pass or fail in words as well as colour. */
function Targets({ d }: { d: SimulationDetail }) {
  const matrices = targetMatrix(d);
  if (matrices.length === 0) return null;
  const conds = conditionsOf(d);
  return (
    <section className="card" aria-label="Targets">
      <h4 style={{ marginTop: 0 }}>Targets</h4>
      {matrices.map((m) => (
        <div key={m.measure} style={{ marginBottom: 8 }}>
          <div>
            <b>{m.measure}</b> {m.range}
            {m.why && <span className="faint"> — {m.why}</span>}
          </div>
          <table className="dtable" aria-label={`Target ${m.measure}`}>
            <thead>
              <tr>
                <th>Variation</th>
                {conds.map((c) => (
                  // Names carry units ("10 µF", "5 mΩ"): never in capitals.
                  <th key={c} style={{ textTransform: 'none' }}>
                    {c}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {m.rows.map((r) => (
                <tr key={r.variation}>
                  <td>
                    {r.variation}
                    {r.change && <div className="faint" style={{ fontSize: 11 }}>{r.change}</div>}
                  </td>
                  {r.cells.map((c) => (
                    <td key={c.condition}>
                      {c.value || <span className="faint">not run</span>}{' '}
                      {c.pass === true && (
                        <span className="badge ok">
                          <CheckCircle2 size={11} /> pass
                        </span>
                      )}
                      {c.pass === false && (
                        <span className="badge err">
                          <XCircle size={11} /> fail
                        </span>
                      )}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ))}
    </section>
  );
}

/** A variation: what changed, the agent's note, each condition's latest run, its netlist; run it again or delete it. */
function VariationCard({ project, test, v, n, note, canRun }: { project: string; test: string; v: VariationDetail; n: number; note?: string; canRun: boolean }) {
  const [busy, setBusy] = useState(false);
  const va = v.variation;
  return (
    <div className="card" aria-label={`Variation ${va.name}`}>
      <div className="row" style={{ gap: 8, alignItems: 'baseline' }}>
        <b>
          {n}. {va.name}
        </b>
        {va.change && <span className="muted">{va.change}</span>}
        <span className="grow" />
        <span className="faint" style={{ fontSize: 12 }}>{relTime(va.updated)}</span>
        <button
          className="btn sm"
          disabled={!canRun || busy}
          title={canRun ? `Run ${va.name} again in every condition` : 'ngspice isn’t set up: see Settings'}
          onClick={async () => {
            setBusy(true);
            try {
              await api.runSimulation(project, test, va.name);
            } finally {
              setBusy(false);
            }
          }}
        >
          <Play size={13} /> {busy ? 'Running…' : 'Run again'}
        </button>
        <button
          className="btn sm danger"
          title={`Delete ${va.name} and its results; the test's other variations stay`}
          onClick={async () => {
            if (!window.confirm(`Delete the variation ${va.name} and its results?`)) return;
            await api.deleteVariation(project, test, va.name);
          }}
        >
          <Trash2 size={13} />
        </button>
      </div>
      {note && <Markdown src={note} />}
      <Runs runs={v.latest} />
      <details>
        <summary style={{ fontSize: 12 }}>
          Netlist{va.conditions.length > 0 ? ` and ${va.conditions.length} conditions` : ''}
          {va.models.length > 0 ? `, ${va.models.length} models` : ''}
        </summary>
        <pre className="mono" style={{ whiteSpace: 'pre-wrap', fontSize: 12 }}>{va.netlist}</pre>
        {va.conditions.map((c) => (
          <div key={c.name} style={{ fontSize: 12 }}>
            <b>{c.name}</b>: {Object.entries(c.params ?? {}).map(([k, val]) => `${k} = ${val}`).join(', ')}
            {c.temp !== undefined ? ` at ${c.temp} °C` : ''}
          </div>
        ))}
        {va.models.length > 0 && <div style={{ fontSize: 12 }}>Models: {va.models.map((m) => `${m.part} ${m.name}`).join(', ')}</div>}
      </details>
    </div>
  );
}

/** Each condition's latest run: state, measurements, how long, what ngspice said. */
function Runs({ runs }: { runs: SimRun[] }) {
  if (runs.length === 0) return <div className="muted" style={{ fontSize: 12 }}>Not run yet.</div>;
  return (
    <table className="dtable" aria-label="Runs" style={{ marginTop: 6 }}>
      <thead>
        <tr>
          <th>Condition</th>
          <th>State</th>
          <th>Measurements</th>
          <th>ngspice</th>
        </tr>
      </thead>
      <tbody>
        {runs.map((r) => {
          const b = stateBadge(r.state);
          return (
            <tr key={r.id}>
              <td>{r.condition}</td>
              <td>
                <span className={`badge ${b.tone}`}>{b.label}</span>
                {r.error && <div className="err-text" style={{ fontSize: 12 }}>{r.error}</div>}
              </td>
              <td style={{ fontSize: 12 }}>
                {r.measures.map((m) => (
                  <div key={m.name}>
                    {m.name} = {m.value !== undefined ? formatValue(m.value) : <span className="muted">{m.error}</span>}
                  </div>
                ))}
              </td>
              <td className="faint" style={{ fontSize: 12 }}>
                {r.ngspice} · {r.tookMs} ms
                {r.problems.length > 0 && <div title={r.problems.join('\n')}>{r.problems.length} message{r.problems.length === 1 ? '' : 's'}</div>}
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

/** The agent's charts. */
function Charts({ d }: { d: SimulationDetail }) {
  const ps = panels(d.simulation.presentation?.plots ?? []);
  return (
    <div className="col" style={{ gap: 12 }}>
      {ps.map((p) => (
        <ChartPanel key={p.key} panel={p} d={d} />
      ))}
    </div>
  );
}

/** One chart: the variations it compares (chosen with the chips above it, starting with the agent's choice) in its conditions, loaded and drawn. */
function ChartPanel({ panel, d }: { panel: Panel; d: SimulationDetail }) {
  const all = d.variations.map((v) => v.variation.name);
  const [chosen, setChosen] = useState<string[]>(() => defaultVariations(panel.spec, d.variations));
  const runs = runsFor(panel.spec, d.variations, chosen);
  const ids = runs.map((r) => r.id).join(',');
  const analyses = d.variations.find((v) => chosen.includes(v.variation.name))?.analyses ?? d.variations.at(-1)?.analyses ?? [];
  const data = useAsync(
    async (): Promise<ChartModel | null> => {
      if (runs.length === 0) return null;
      const got = await Promise.all(runs.map((r) => api.simulationData(r.id, panel.spec.plot, panel.spec.signals, panel.part)));
      const m = chartModel(panel, got, darkTheme());
      return { ...m, xLabel: axisName(m.xLabel, panel.spec.plot, analyses) };
    },
    [ids, panel.key],
  );
  const toggle = (name: string) => setChosen((c) => (c.includes(name) ? (c.length > 1 ? c.filter((x) => x !== name) : c) : all.filter((x) => c.includes(x) || x === name)));
  const chips =
    all.length > 1 ? (
      <div className="row wrap" role="group" aria-label={`${panel.title}: variations`} style={{ gap: 6, marginBottom: 6, fontSize: 12 }}>
        <span className="muted">Compare:</span>
        {all.map((n) => (
          <button key={n} className={`btn sm${chosen.includes(n) ? ' on' : ''}`} aria-pressed={chosen.includes(n)} onClick={() => toggle(n)}>
            {n}
          </button>
        ))}
      </div>
    ) : null;
  let body;
  if (runs.length === 0) body = <div className="card muted">{panel.title}: no run of {chosen.join(', ')} has results for {panel.spec.plot} yet.</div>;
  else if (data.error) body = <ErrorBox error={data.error} />;
  else if (!data.data) body = <Spinner label={`Loading ${panel.title}…`} />;
  else body = <SimChart model={data.data} title={panel.title} note={panel.spec.note} />;
  return (
    <div>
      {chips}
      {body}
    </div>
  );
}
