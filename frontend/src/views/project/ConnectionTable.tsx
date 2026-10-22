import { useEffect, useState } from 'react';
import { Download, Plus, Trash2, X } from 'lucide-react';
import { api, saveText } from '../../lib/api';
import { useStore } from '../../lib/store';
import type { ConnectionRow, Pin, ProjectDetail } from '../../lib/types';
import { Checks } from '../../components/Checks';
import { change, marked } from './connections';

/** What an entry can be connected as: its designators, and each one when it has several. */
export function designators(d: string): string[] {
  const each = d.split(/[,;\s]+/).filter(Boolean);
  return each.length > 1 ? [d, ...each] : [d];
}

/** The CSV of a connection table. */
export function connectionsCSV(d: ProjectDetail): string {
  const q = (v: unknown) => {
    const t = String(v ?? '');
    return /[",\n]/.test(t) ? `"${t.replace(/"/g, '""')}"` : t;
  };
  const nets = new Map(d.connections.nets.map((n) => [n.id, n]));
  const lines = [['Net', 'Kind', 'Bus', 'Part', 'Pin', 'Pin name', 'Pin type', 'Notes'].join(',')];
  for (const r of d.connections.connections) {
    const n = nets.get(r.netId);
    lines.push([r.net, n?.kind, n?.bus, r.endpoint, r.pin, r.pinName, r.pinType, r.notes].map(q).join(','));
  }
  return lines.join('\n') + '\n';
}

/**
 * The connection table: each net with the pins on it, editable, with the
 * checks against the parts' pin tables on top.
 */
export function ConnectionTable({ d, highlight, onChange }: { d: ProjectDetail; highlight?: string; onChange: () => void }) {
  const s = useStore();
  const c = d.connections;
  const project = d.project.name;
  const checks = (d.checks ?? []).filter((x) => x.area === 'connections');
  const [net, setNet] = useState('');
  const [part, setPart] = useState('');
  const [pin, setPin] = useState('');
  const [block, setBlock] = useState('');
  const [blockKind, setBlockKind] = useState('');
  const [blockValue, setBlockValue] = useState('');
  const [pins, setPins] = useState<Pin[]>([]);
  const entry = d.parts.find((p) => designators(p.designator || p.mpn).includes(part));
  useEffect(() => {
    setPins([]);
    if (entry?.datasheetId) api.pins(entry.datasheetId).then(setPins, () => {});
  }, [entry?.datasheetId]);
  const hl = highlight?.toLowerCase();
  const add = () => {
    if (!net.trim() || !part) return;
    change(project, { connect: [{ net: net.trim(), part, pin: pin.trim() || undefined }] }, onChange);
    setPin('');
  };
  const remove = (r: ConnectionRow) => change(project, { connect: [{ net: r.net, part: r.endpoint, pin: r.pin, remove: true }] }, onChange);
  const exportCSV = async () => {
    const where = await saveText(`${project}-connections.csv`, connectionsCSV(d), 'text/csv', !!s.app?.canSaveFile);
    if (where) s.toast({ kind: 'ok', title: 'Connections saved', body: where });
  };
  // Each designator of an entry with several ("R1,R2") can be connected
  // on its own, as well as the entry as a whole.
  const endpoints = [...d.parts.flatMap((p) => designators(p.designator || p.mpn)), ...c.blocks.map((b) => b.name)];
  return (
    <div className="col" style={{ gap: 16 }}>
      {checks.length > 0 && (
        <div className="card">
          <h3>Checks against the pin tables</h3>
          <Checks checks={checks} />
        </div>
      )}
      <div className="card col" style={{ gap: 8 }}>
        <div className="row">
          <h3 className="grow" style={{ margin: 0 }}>
            Connections
          </h3>
          <button className="btn sm" onClick={exportCSV} disabled={c.connections.length === 0}>
            <Download size={13} /> CSV
          </button>
        </div>
        <form
          className="row"
          style={{ flexWrap: 'wrap' }}
          onSubmit={(e) => {
            e.preventDefault();
            add();
          }}
        >
          <input className="input" style={{ width: 140 }} placeholder="Net (SDA, 3V3…)" aria-label="Net" list="sd-nets" value={net} onChange={(e) => setNet(e.target.value)} />
          <datalist id="sd-nets">
            {c.nets.map((n) => (
              <option key={n.id} value={n.name} />
            ))}
          </datalist>
          <select className="input" style={{ width: 150 }} aria-label="Part or block" value={part} onChange={(e) => setPart(e.target.value)}>
            <option value="">Part or block…</option>
            {endpoints.map((e) => (
              <option key={e} value={e}>
                {e}
              </option>
            ))}
          </select>
          <input className="input" style={{ width: 150 }} placeholder={entry ? 'Pin (number or name)' : 'Pin'} aria-label="Pin" list="sd-pins" value={pin} onChange={(e) => setPin(e.target.value)} />
          <datalist id="sd-pins">
            {pins.map((p) => (
              <option key={p.id} value={p.number}>
                {p.name}
                {p.package ? ` (${p.package})` : ''}
              </option>
            ))}
          </datalist>
          <button className="btn sm primary" type="submit" disabled={!net.trim() || !part}>
            <Plus size={13} /> Connect
          </button>
        </form>
        {c.nets.length === 0 ? (
          <div className="muted">No connections yet. Put pins on nets above, or ask your agent to draft them (set_connections).</div>
        ) : (
          <table className="dtable">
            <thead>
              <tr>
                <th>Net</th>
                <th>Part</th>
                <th>Pin</th>
                <th>Name</th>
                <th>Type</th>
                <th>Notes</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {c.nets.map((n) => {
                const rows = c.connections.filter((r) => r.netId === n.id);
                const on = marked(hl, n.name, n.bus);
                return [
                  <tr key={`n${n.id}`} className={on ? 'agent-hl' : undefined}>
                    <td colSpan={7}>
                      <div className="row">
                        <b className="mono">{n.name}</b>
                        <select
                          className="input"
                          aria-label={`${n.name} kind`}
                          value={n.kind}
                          onChange={(e) => change(project, { nets: [{ name: n.name, kind: e.target.value }] }, onChange)}
                          style={{ width: 90, height: 26 }}
                        >
                          <option value="signal">signal</option>
                          <option value="power">power</option>
                          <option value="ground">ground</option>
                        </select>
                        <input
                          className="input"
                          style={{ width: 110, height: 26 }}
                          placeholder="Bus (I2C1)"
                          aria-label={`${n.name} bus`}
                          defaultValue={n.bus ?? ''}
                          onBlur={(e) => e.target.value !== (n.bus ?? '') && change(project, { nets: [{ name: n.name, bus: e.target.value }] }, onChange)}
                        />
                        <span className="grow" />
                        <button className="btn ghost icon sm" title={`Delete ${n.name} and its connections`} onClick={() => change(project, { nets: [{ name: n.name, remove: true }] }, onChange)}>
                          <Trash2 size={12} />
                        </button>
                      </div>
                    </td>
                  </tr>,
                  ...rows.map((r) => (
                    <tr key={r.id} className={marked(hl, r.endpoint) ? 'agent-hl' : undefined}>
                      <td />
                      <td>{r.endpoint}</td>
                      <td className="mono">{r.pin}</td>
                      <td className="mono">{r.pinName}</td>
                      <td className="faint">{r.pinType}</td>
                      <td className="muted">{r.notes}</td>
                      <td>
                        <button className="btn ghost icon sm" title="Disconnect" onClick={() => remove(r)}>
                          <X size={12} />
                        </button>
                      </td>
                    </tr>
                  )),
                ];
              })}
            </tbody>
          </table>
        )}
      </div>
      <div className="card col" style={{ gap: 8 }}>
        <h3 style={{ margin: 0 }}>Blocks off the BOM</h3>
        <div className="muted">A connector, a battery, a module still to be chosen, or a passive with its value (R4 36.5k, C1 10µF): what the block diagram shows that isn't a part in the BOM.</div>
        <div className="row" style={{ flexWrap: 'wrap' }}>
          {c.blocks.map((b) => (
            <span key={b.id} className="chip">
              {b.name}
              {b.value ? <span className="faint">{b.value}</span> : null}
              <button className="btn ghost icon sm" title={`Remove ${b.name}`} onClick={() => change(project, { blocks: [{ name: b.name, remove: true }] }, onChange)}>
                <X size={11} />
              </button>
            </span>
          ))}
          <form
            className="row"
            onSubmit={(e) => {
              e.preventDefault();
              if (block.trim()) change(project, { blocks: [{ name: block.trim(), kind: blockKind || undefined, value: blockValue.trim() || undefined }] }, onChange);
              setBlock('');
              setBlockValue('');
            }}
          >
            <input className="input" style={{ width: 140 }} placeholder="J1 USB-C, R4…" aria-label="New block" value={block} onChange={(e) => setBlock(e.target.value)} />
            <select className="input" style={{ width: 120 }} aria-label="Block kind" value={blockKind} onChange={(e) => setBlockKind(e.target.value)}>
              <option value="">block</option>
              {['connector', 'module', 'resistor', 'capacitor', 'inductor', 'diode'].map((k) => (
                <option key={k} value={k}>
                  {k}
                </option>
              ))}
            </select>
            <input className="input" style={{ width: 90 }} placeholder="Value" aria-label="Block value" value={blockValue} onChange={(e) => setBlockValue(e.target.value)} />
            <button className="btn sm" type="submit" disabled={!block.trim()}>
              <Plus size={13} /> Block
            </button>
          </form>
        </div>
      </div>
    </div>
  );
}
