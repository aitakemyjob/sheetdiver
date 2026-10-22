import { Download, LayoutGrid } from 'lucide-react';
import { saveText } from '../../lib/api';
import { useStore } from '../../lib/store';
import type { ProjectDetail } from '../../lib/types';
import { Empty } from '../../components/ui';
import { marked } from './connections';

/** The CSV of the pin tables: one row per pin of each part. */
export function pinoutsCSV(d: ProjectDetail): string {
  const q = (v: unknown) => {
    const t = String(v ?? '');
    return /[",\n]/.test(t) ? `"${t.replace(/"/g, '""')}"` : t;
  };
  const lines = [['Part', 'Pin', 'Name', 'Type', 'Net', 'Connected to', 'Description'].join(',')];
  for (const po of d.connections.pinouts ?? []) for (const p of po.pins) lines.push([po.title, p.number, p.name, p.type, p.net, p.to.join('; '), p.description].map(q).join(','));
  return lines.join('\n') + '\n';
}

/**
 * A reference table per part: every pin (from its datasheet's pin table,
 * connected or not) with its net and what else is on it.
 */
export function PinTables({ d, highlight }: { d: ProjectDetail; highlight?: string }) {
  const s = useStore();
  const pinouts = d.connections.pinouts ?? [];
  const hl = highlight?.toLowerCase();
  if (pinouts.length === 0) {
    return (
      <Empty icon={<LayoutGrid size={28} />} title="No pins yet">
        Parts with a pin table in their datasheet show here, with what each pin is connected to.
      </Empty>
    );
  }
  const exportCSV = async () => {
    const where = await saveText(`${d.project.name}-pins.csv`, pinoutsCSV(d), 'text/csv', !!s.app?.canSaveFile);
    if (where) s.toast({ kind: 'ok', title: 'Pin tables saved', body: where });
  };
  return (
    <div className="col" style={{ gap: 16 }}>
      <div className="row">
        <span className="faint grow">Every pin of each part, from its datasheet's pin table, and what it's connected to. Edit connections in the Connections tab.</span>
        <button className="btn sm" onClick={exportCSV}>
          <Download size={13} /> CSV
        </button>
      </div>
      {pinouts.map((po) => {
        const pp = d.parts.find((p) => `part:${p.id}` === po.node);
        const open = pp ? () => s.open({ kind: 'part', title: pp.mpn, params: { part: pp.mpn, tab: 'pins' } }) : undefined;
        const connected = po.pins.filter((p) => p.net).length;
        return (
          <div key={po.node} className={`card${marked(hl, po.title, po.title.split(' ')[0], po.mpn) ? ' agent-hl' : ''}`}>
            <div className="row" style={{ margin: '0 0 8px', fontWeight: 600 }}>
              {open ? (
                <button className="link" onClick={open} title="Open its pin table">
                  {po.title}
                </button>
              ) : (
                po.title
              )}
              <span className="faint" style={{ fontWeight: 400, fontSize: 12 }}>
                {connected} of {po.pins.length} pins connected
              </span>
            </div>
            <table className="dtable">
              <thead>
                <tr>
                  <th>Pin</th>
                  <th>Name</th>
                  <th>Type</th>
                  <th>Net</th>
                  <th>Connected to</th>
                  <th>Description</th>
                </tr>
              </thead>
              <tbody>
                {po.pins.map((p, i) => (
                  <tr key={i} className={marked(hl, p.net) ? 'agent-hl' : undefined} style={p.net ? undefined : { opacity: 0.6 }}>
                    <td className="mono">{p.number}</td>
                    <td className="mono">{p.name}</td>
                    <td className="faint">{p.type}</td>
                    <td className="mono">{p.net ? p.net : <span className="faint">not connected</span>}</td>
                    <td>{p.netKind === 'ground' ? <span className="faint">ground</span> : p.to.join(', ')}</td>
                    <td className="muted" style={{ fontSize: 12 }}>
                      {p.description}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        );
      })}
    </div>
  );
}
