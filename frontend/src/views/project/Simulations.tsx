import { useEffect, useState } from 'react';
import { AlertTriangle, CheckCircle2, FlaskConical, Play, Trash2, XCircle } from 'lucide-react';
import { api } from '../../lib/api';
import { relTime } from '../../lib/format';
import { axisName, chartModel, formatValue, panels, runsFor, stateBadge, targetRows, type ChartModel, type Panel } from '../../lib/simulation';
import { darkTheme } from '../../lib/diagramPalette';
import { useStore } from '../../lib/store';
import type { NgspiceState, SimRun, SimulationDetail, SimulationSummary } from '../../lib/types';
import { Empty, ErrorBox, Markdown, Spinner, useAsync } from '../../components/ui';
import { SimChart } from '../../components/SimChart';

/**
 * A project's simulations: the list of what the agent simulated, and the
 * one picked with its runs, targets, the agent's write-up and charts.
 */
export function Simulations({ project, highlight }: { project: string; highlight?: string }) {
  const s = useStore();
  const list = useAsync(() => api.simulations(project), [project, s.projectVersion]);
  const ngspice = useAsync(() => api.ngspice(), [s.app?.config.settings.ngspicePath]);
  const [picked, setPicked] = useState<string | undefined>(undefined);
  const sims = list.data ?? [];
  useEffect(() => {
    if (highlight) setPicked(sims.find((x) => x.name.toLowerCase() === highlight)?.name ?? picked);
  }, [highlight, list.data]); // eslint-disable-line react-hooks/exhaustive-deps
  if (list.loading && !list.data) return <Spinner label="Loading simulations…" />;
  if (list.error) return <ErrorBox error={list.error} />;
  if (sims.length === 0)
    return (
      <Empty icon={<FlaskConical size={28} />} title="No simulations yet">
        Ask your agent to simulate this board — start-up, inrush, a load step, loop gain, corners — and it sets up and runs them here with ngspice.
        <NgspiceLine state={ngspice.data} />
      </Empty>
    );
  const name = picked ?? sims[0].name;
  return (
    <div className="row" style={{ alignItems: 'flex-start', gap: 16 }}>
      <nav aria-label="Simulations" className="col" style={{ width: 260, flexShrink: 0, gap: 6 }}>
        {sims.map((sm) => (
          <SimulationItem key={sm.name} sim={sm} on={sm.name === name} onPick={() => setPicked(sm.name)} />
        ))}
        <NgspiceLine state={ngspice.data} />
      </nav>
      <div className="grow" style={{ minWidth: 0 }}>
        <SimulationPanel project={project} name={name} version={s.projectVersion} canRun={!!ngspice.data?.version} onDeleted={() => setPicked(undefined)} />
      </div>
    </div>
  );
}

/** A simulation in the list: name, kind, its variants' states and targets. */
function SimulationItem({ sim, on, onPick }: { sim: SimulationSummary; on: boolean; onPick: () => void }) {
  const states = Object.values(sim.states);
  const failed = states.filter((st) => st === 'failed').length;
  return (
    <button className={`card sim-item${on ? ' on' : ''}`} aria-current={on} onClick={onPick} style={{ textAlign: 'left', padding: 10 }}>
      <div style={{ fontWeight: 600 }}>{sim.name}</div>
      {sim.kind && <div className="muted" style={{ fontSize: 12 }}>{sim.kind}</div>}
      <div className="row wrap" style={{ gap: 6, marginTop: 4, fontSize: 12 }}>
        <span className="muted">
          {states.length} run{states.length === 1 ? '' : 's'}
        </span>
        {failed > 0 && <span className="badge err">{failed} failed</span>}
        {sim.pass > 0 && <span className="badge ok">{sim.pass} pass</span>}
        {sim.fail > 0 && <span className="badge err">{sim.fail} fail</span>}
        {!sim.presented && states.length > 0 && <span className="badge">not presented</span>}
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

/** A simulation: what it's for, its runs, its targets, the agent's write-up and charts. */
function SimulationPanel({ project, name, version, canRun, onDeleted }: { project: string; name: string; version: number; canRun: boolean; onDeleted: () => void }) {
  const detail = useAsync(() => api.simulation(project, name), [project, name, version]);
  const [busy, setBusy] = useState(false);
  const d = detail.data;
  if (detail.loading && !d) return <Spinner label="Loading…" />;
  if (detail.error) return <ErrorBox error={detail.error} />;
  if (!d) return null;
  const sim = d.simulation;
  const p = sim.presentation;
  const run = async () => {
    setBusy(true);
    try {
      await api.runSimulation(project, name);
    } finally {
      setBusy(false);
    }
  };
  return (
    <article className="col" style={{ gap: 12 }} aria-label={`Simulation ${sim.name}`}>
      <header className="col" style={{ gap: 4 }}>
        <h2 className="row" style={{ margin: 0, gap: 8 }}>
          {sim.name}
          {sim.kind && <span className="badge">{sim.kind}</span>}
          <span className="grow" />
          <button className="btn sm" disabled={!canRun || busy} title={canRun ? 'Run every variant again' : 'ngspice isn’t set up: see Settings'} onClick={run}>
            <Play size={13} /> {busy ? 'Running…' : 'Run again'}
          </button>
          <button
            className="btn sm danger"
            onClick={async () => {
              if (!window.confirm(`Delete the simulation ${sim.name} and its results?`)) return;
              await api.deleteSimulation(project, name);
              onDeleted();
            }}
          >
            <Trash2 size={13} /> Delete
          </button>
        </h2>
        {sim.purpose && <div>{sim.purpose}</div>}
        <div className="faint" style={{ fontSize: 12 }}>
          {sim.target ? `Tests ${sim.target} on its own` : 'The board'} · {d.analyses.map((a) => a.kind).join(', ')} · set up by {sim.source} {relTime(sim.updated)}
        </div>
      </header>
      {p ? (
        <section className="card" aria-label="Presentation">
          <Markdown src={p.summary} />
          {(p.standIns?.length ?? 0) > 0 && (
            <div className="muted" style={{ fontSize: 12 }}>
              <AlertTriangle size={12} /> Stand-ins, not real models: {p.standIns!.join('; ')}
            </div>
          )}
        </section>
      ) : (
        <div className="muted">The agent hasn’t written up these results yet.</div>
      )}
      <Targets d={d} />
      {p && <Charts d={d} />}
      {p?.conclusions && (
        <section className="card" aria-label="Conclusions">
          <h4 style={{ marginTop: 0 }}>Conclusions</h4>
          <Markdown src={p.conclusions} />
        </section>
      )}
      <Runs runs={d.latest} />
      <details className="card">
        <summary>Netlist{sim.variants.length > 0 ? ` and ${sim.variants.length} variants` : ''}</summary>
        <pre className="mono" style={{ whiteSpace: 'pre-wrap', fontSize: 12 }}>{sim.netlist}</pre>
        {sim.variants.map((v) => (
          <div key={v.name} style={{ fontSize: 12 }}>
            <b>{v.name}</b>: {Object.entries(v.params ?? {}).map(([k, val]) => `${k} = ${val}`).join(', ')}
            {v.temp !== undefined ? ` at ${v.temp} °C` : ''}
          </div>
        ))}
        {sim.models.length > 0 && <div style={{ fontSize: 12 }}>Models: {sim.models.map((m) => `${m.part} ${m.name}`).join(', ')}</div>}
      </details>
    </article>
  );
}

/** The targets: each measurement with its range and every variant's value, pass or fail in words as well as colour. */
function Targets({ d }: { d: SimulationDetail }) {
  const rows = targetRows(d.checks);
  if (rows.length === 0) return null;
  const variants = [...new Set(d.checks.map((c) => c.variant))];
  return (
    <section className="card" aria-label="Targets">
      <h4 style={{ marginTop: 0 }}>Targets</h4>
      <table className="dtable">
        <thead>
          <tr>
            <th>Measurement</th>
            <th>Target</th>
            {variants.map((v) => (
              // Variant names carry units ("10 µF", "5 mΩ"): never in capitals.
              <th key={v} style={{ textTransform: 'none' }}>
                {v}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.measure}>
              <td title={r.why}>
                {r.measure}
                {r.why && <div className="faint" style={{ fontSize: 11 }}>{r.why}</div>}
              </td>
              <td>{r.range}</td>
              {variants.map((v) => {
                const res = r.results.find((x) => x.variant === v);
                return (
                  <td key={v}>
                    {res?.value ?? '–'}{' '}
                    {res?.pass === true && (
                      <span className="badge ok">
                        <CheckCircle2 size={11} /> pass
                      </span>
                    )}
                    {res?.pass === false && (
                      <span className="badge err">
                        <XCircle size={11} /> fail
                      </span>
                    )}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}

/** Each variant's latest run: state, measurements, how long, what ngspice said. */
function Runs({ runs }: { runs: SimRun[] }) {
  if (runs.length === 0) return <div className="muted">Not run yet.</div>;
  return (
    <section className="card" aria-label="Runs">
      <h4 style={{ marginTop: 0 }}>Runs</h4>
      <table className="dtable">
        <thead>
          <tr>
            <th>Variant</th>
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
                <td>{r.variant}</td>
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
    </section>
  );
}

/** The agent's charts, each from its runs' data. */
function Charts({ d }: { d: SimulationDetail }) {
  const ps = panels(d.simulation.presentation?.plots ?? []);
  return (
    <div className="col" style={{ gap: 12 }}>
      {ps.map((p) => (
        <ChartPanel key={p.key} panel={p} runs={runsFor(p.spec, d.latest)} analyses={d.analyses} />
      ))}
    </div>
  );
}

/** One chart: its runs' signals loaded and drawn. */
function ChartPanel({ panel, runs, analyses }: { panel: Panel; runs: SimRun[]; analyses: SimulationDetail['analyses'] }) {
  const ids = runs.map((r) => r.id).join(',');
  const data = useAsync(
    async (): Promise<ChartModel | null> => {
      if (runs.length === 0) return null;
      const all = await Promise.all(runs.map((r) => api.simulationData(r.id, panel.spec.plot, panel.spec.signals, panel.part)));
      const m = chartModel(panel, all, darkTheme());
      return { ...m, xLabel: axisName(m.xLabel, panel.spec.plot, analyses) };
    },
    [ids, panel.key],
  );
  if (runs.length === 0)
    return (
      <div className="card muted">
        {panel.title}: no run has results for {panel.spec.plot} yet.
      </div>
    );
  if (data.error) return <ErrorBox error={data.error} />;
  if (!data.data) return <Spinner label={`Loading ${panel.title}…`} />;
  return <SimChart model={data.data} title={panel.title} note={panel.spec.note} />;
}
