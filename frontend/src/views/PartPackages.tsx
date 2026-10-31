import { useState } from 'react';
import { AlertTriangle, Trash2 } from 'lucide-react';
import { ErrorBox, Spinner, useAsync } from '../components/ui';
import { api } from '../lib/api';
import { dimLabel, limitCell, packageTitle, recordedRows, sourceBadge } from '../lib/packages';
import { useStore } from '../lib/store';
import type { PackageDim, PartPackage } from '../lib/types';

/**
 * A part's Package tab: each package's outline dimensions and land
 * pattern as the datasheet gives them (read by SheetDiver, or recorded
 * by an agent from the drawing), with what doesn't add up.
 */
export function PartPackagesPanel({ part, onPage }: { part: string; onPage: (page: number) => void }) {
  const s = useStore();
  const [reloaded, setReloaded] = useState(0);
  const pk = useAsync(() => api.partPackages(part), [part, s.libraryVersion, reloaded]);
  const d = pk.data;
  if (pk.loading && !d) return <Spinner label="Reading…" />;
  if (pk.error) return <ErrorBox error={pk.error} />;
  if (!d) return null;
  const packages = d.packages ?? [];
  if (packages.length === 0) {
    return (
      <div className="muted" style={{ fontSize: 13 }}>
        No package dimensions were read from this datasheet. Ask your agent to read the package drawing and record it.
      </div>
    );
  }
  const remove = (id: number) => {
    if (!window.confirm('Remove these recorded dimensions? The datasheet’s own, if any, stand again.')) return;
    api
      .deletePackage(id)
      .then(() => setReloaded((n) => n + 1))
      .catch((e: Error) => s.toast({ kind: 'err', title: 'Could not remove', body: e.message }));
  };
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      {packages.map((p) => (
        <PackageCard key={p.name} pkg={p} onPage={onPage} onRemove={remove} />
      ))}
    </div>
  );
}

/** One package. */
function PackageCard({ pkg, onPage, onRemove }: { pkg: PartPackage; onPage: (page: number) => void; onRemove: (id: number) => void }) {
  const dims = pkg.dims ?? [];
  const land = pkg.land ?? [];
  return (
    <div className="card" style={{ padding: 12 }}>
      <div className="row" style={{ gap: 8, alignItems: 'baseline', flexWrap: 'wrap' }}>
        <b>{packageTitle(pkg)}</b>
        {!!pkg.pins && <span className="muted">{pkg.pins} {pkg.pins === 1 ? 'pin' : 'pins'} in the pin table</span>}
        <span className="grow" />
        <button className="btn sm" onClick={() => onPage(pkg.page)}>
          Page {pkg.page + 1}
        </button>
        {recordedRows(pkg).map((r) => (
          <button key={r.id} className="btn sm" title={`Remove what the ${r.source} recorded`} onClick={() => onRemove(r.id)}>
            <Trash2 size={13} /> {r.source}
          </button>
        ))}
      </div>
      {pkg.checks?.map((c) => (
        <div key={c} className="row warn-text" style={{ gap: 6, fontSize: 12, marginTop: 6 }}>
          <AlertTriangle size={13} /> {c}
        </div>
      ))}
      {dims.length > 0 && <DimTable title="Outline" dims={dims} />}
      {land.length > 0 && <DimTable title="Land pattern" dims={land} />}
      {dims.length === 0 && land.length === 0 && !!pkg.candidates?.length && (
        <div style={{ marginTop: 8, fontSize: 12 }}>
          <div className="muted">No dimension table: the numbers are on the drawing. Found on it:</div>
          <div style={{ fontFamily: 'var(--font-mono)' }}>{pkg.candidates.join(' · ')}</div>
        </div>
      )}
      {pkg.notes && <div className="muted" style={{ fontSize: 12, marginTop: 6 }}>{pkg.notes}</div>}
    </div>
  );
}

/** A table of dimensions in mm. */
function DimTable({ title, dims }: { title: string; dims: PackageDim[] }) {
  return (
    <div style={{ marginTop: 10 }}>
      <div className="muted" style={{ fontSize: 12, marginBottom: 4 }}>
        {title} (mm)
      </div>
      <table className="dtable">
        <thead>
          <tr>
            <th>Symbol</th>
            <th>Dimension</th>
            <th style={{ textAlign: 'right' }}>Min</th>
            <th style={{ textAlign: 'right' }}>Nom</th>
            <th style={{ textAlign: 'right' }}>Max</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {dims.map((d) => {
            const badge = sourceBadge(d.source);
            return (
              <tr key={d.symbol + (d.key ?? '')}>
                <td>
                  <code>{d.symbol}</code>
                </td>
                <td>{dimLabel(d)}</td>
                <td className="num">{limitCell(d.min)}</td>
                <td className="num">{limitCell(d.nom)}</td>
                <td className="num">{limitCell(d.max)}</td>
                <td>
                  {d.note && <span className="muted">{d.note} </span>}
                  {d.unit && d.unit !== 'mm' && <span className="muted">from {d.unit} </span>}
                  {badge && <span className={`badge ${badge.tone}`}>{badge.label}</span>}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
