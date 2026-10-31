import { useState } from 'react';
import { AlertTriangle, Box, CircuitBoard, FolderOpen, PenTool, RefreshCw, RotateCcw, Shapes } from 'lucide-react';
import { CadDrawing } from '../components/CadDrawing';
import { ErrorBox, Spinner, useAsync } from '../components/ui';
import { api } from '../lib/api';
import { cadActions, editedInLabel, formatsLabel, outlineRows, problemsOf, resetAction, stateInfo } from '../lib/cad';
import { relTime } from '../lib/format';
import { useStore } from '../lib/store';
import type { CadLibrary, CadPackage, CadView, PartCad } from '../lib/types';

/**
 * A part's CAD tab: for each package, its schematic symbol, footprint
 * and 3D model as kept in the user's CAD library, where each came from
 * (SheetDiver, an agent, or the user's own edit), what's wrong between
 * them, and the library they're kept in.
 */
export function PartCadPanel({ part }: { part: string }) {
  const s = useStore();
  const [reloaded, setReloaded] = useState(0);
  const cad = useAsync(() => api.partCad(part), [part, s.libraryVersion, reloaded]);
  const d = cad.data;
  if (cad.loading && !d) return <Spinner label="Drawing…" />;
  if (cad.error) return <ErrorBox error={cad.error} />;
  if (!d) return null;
  const reset = (pkg: CadPackage, kind: 'symbol' | 'footprint') => {
    const p = cadActions.reset(d.part.mpn, pkg.package, kind, kind === 'symbol' ? pkg.symbol : pkg.footprint, (m) => window.confirm(m));
    p?.then(() => setReloaded((n) => n + 1)).catch((e: Error) => s.toast({ kind: 'err', title: 'Could not reset', body: e.message }));
  };
  const sync = () =>
    cadActions
      .sync()
      .then((msg) => (s.toast({ kind: 'ok', title: 'CAD library synced', body: msg }), setReloaded((n) => n + 1)))
      .catch((e: Error) => s.toast({ kind: 'err', title: 'Could not sync the CAD library', body: e.message }));
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <LibraryLine library={d.library} onSync={sync} />
      {d.packages.map((p) => (
        <PackageCard
          key={p.package || '-'}
          cad={d}
          pkg={p}
          onReset={(kind) => reset(p, kind)}
          onEdit={(what) => s.open({ kind: 'cadEdit', title: `${d.part.mpn} ${what === '3d' ? '3D' : what}`, params: { part: d.part.mpn, package: p.package, what } })}
        />
      ))}
    </div>
  );
}

/** Where the library is kept, and syncing it. */
function LibraryLine({ library, onSync }: { library: CadLibrary; onSync: () => void }) {
  return (
    <div className="row muted" style={{ fontSize: 12, gap: 8, flexWrap: 'wrap' }}>
      <FolderOpen size={13} />
      <span>
        Kept in <b title={library.dir}>{library.name}</b> ({formatsLabel(library)}) at <code>{library.dir}</code>
      </span>
      <span className="grow" />
      <button className="btn sm" onClick={onSync} title="Read your edits back from the library, then write it">
        <RefreshCw size={13} /> Sync library
      </button>
    </div>
  );
}

/** A state badge with its explanation. */
function StateBadge({ view }: { view: CadView }) {
  const i = stateInfo(view.state);
  return (
    <span className={`badge ${i.tone}`} title={i.title}>
      {i.label}
    </span>
  );
}

/** One package: symbol, footprint and 3D model side by side, then what's wrong and the outline. */
function PackageCard({ cad, pkg, onReset, onEdit }: { cad: PartCad; pkg: CadPackage; onReset: (kind: 'symbol' | 'footprint') => void; onEdit: (what: 'symbol' | 'footprint' | '3d') => void }) {
  const problems = problemsOf(pkg);
  return (
    <section className="card" aria-label={`Package ${pkg.package || 'unknown'}`}>
      <div className="row" style={{ alignItems: 'baseline', gap: 8 }}>
        <h3 style={{ margin: 0 }}>{pkg.package || 'Package not known'}</h3>
        <span className="muted">{pkg.pinsFrom ? `${pkg.pins} pins, numbered as ${pkg.pinsFrom}: the pin table has no column of its own for it` : `${pkg.pins} pins in the pin table`}</span>
      </div>
      <div className="cad-grid">
        <Pane title="Symbol" icon={<Shapes size={13} />} view={pkg.symbol} empty="No pin table yet: an agent can record it (record_pins), then the symbol is drawn from it." onReset={() => onReset('symbol')} onEdit={pkg.symbol?.symbol ? () => onEdit('symbol') : undefined}>
          {pkg.symbol?.symbol && <CadDrawing drawing={pkg.symbol.drawing} label={`${cad.part.mpn} symbol`} />}
        </Pane>
        <Pane title="Footprint" icon={<CircuitBoard size={13} />} view={pkg.footprint} empty={pkg.missing ?? 'No footprint yet.'} onReset={() => onReset('footprint')} onEdit={pkg.footprint?.footprint ? () => onEdit('footprint') : undefined}>
          {pkg.footprint?.footprint && <CadDrawing drawing={pkg.footprint.drawing} label={`${cad.part.mpn} footprint`} />}
        </Pane>
        <Pane title="3D model" icon={<Box size={13} />} view={pkg.footprint?.model ? pkg.footprint : undefined} empty="Built with the footprint, from the package outline." badge={false} onEdit={pkg.footprint?.model ? () => onEdit('3d') : undefined}>
          {pkg.footprint?.model && <CadDrawing drawing={pkg.footprint.model} label={`${cad.part.mpn} 3D model`} />}
          {pkg.footprint?.modelKept && <div className="muted" style={{ fontSize: 12 }}>Your own model file is kept.</div>}
          {pkg.footprint?.model3dEdited && <div className="muted" style={{ fontSize: 12 }}>Edited in SheetDiver: kept whoever edits the footprint.</div>}
        </Pane>
      </div>
      {problems.length > 0 && (
        <ul className="cad-problems" aria-label="Problems">
          {problems.map((x, i) => (
            <li key={i} className={x.level === 'error' ? 'err-text' : 'warn-text'}>
              <AlertTriangle size={12} /> <b>{x.about}:</b> {x.text}
            </li>
          ))}
        </ul>
      )}
      <Outline view={pkg.footprint} />
    </section>
  );
}

/** A symbol's, footprint's or model's pane: its state, where it came from, notes and the drawing. */
function Pane({ title, icon, view, empty, onReset, onEdit, badge = true, children }: { title: string; icon: React.ReactNode; view?: CadView; empty: string; onReset?: () => void; onEdit?: () => void; badge?: boolean; children: React.ReactNode }) {
  const reset = resetAction(view);
  return (
    <div className="cad-pane">
      <div className="row" style={{ gap: 6, minHeight: 24 }}>
        {icon}
        <b>{title}</b>
        {view && badge && <StateBadge view={view} />}
        <span className="grow" />
        {onEdit && (
          <button className="btn ghost sm" onClick={onEdit} title={`Edit the ${title.toLowerCase()} in SheetDiver`} aria-label={`Edit the ${title.toLowerCase()}`}>
            <PenTool size={12} /> Edit
          </button>
        )}
        {badge && reset && onReset && (
          <button className="btn ghost sm" onClick={onReset} title={reset.confirm}>
            <RotateCcw size={12} /> {reset.label}
          </button>
        )}
      </div>
      {view ? (
        <>
          {children}
          {badge && (
            <div className="muted" style={{ fontSize: 12 }}>
              <code>{view.name}</code>
              {editedInLabel(view) && <> · {editedInLabel(view)}</>}
              {view.footprint && view.state !== 'edited' && <> · pads {view.footprint.from}</>}
              {view.standard && <> · standard outline, not yet checked against the datasheet</>}
              {!!view.datasheetPage && <> · from the datasheet's package dimensions (page {view.datasheetPage})</>}
              {view.updated && <> · {relTime(view.updated)}</>}
            </div>
          )}
          {badge && view.notes && <div style={{ fontSize: 12 }}>{view.notes}</div>}
        </>
      ) : (
        <div className="cad-empty muted">{empty}</div>
      )}
    </div>
  );
}

/** The package outline a footprint is drawn from. */
function Outline({ view }: { view?: CadView }) {
  const rows = outlineRows(view?.package);
  if (!rows.length) return null;
  return (
    <details>
      <summary className="muted" style={{ fontSize: 12, cursor: 'pointer' }}>
        Package outline the footprint is drawn from (mm)
      </summary>
      <table className="dtable" aria-label="Package outline">
        <tbody>
          {rows.map(([what, sym, v]) => (
            <tr key={what}>
              <td>{what}</td>
              <td className="muted">{sym}</td>
              <td>{v}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </details>
  );
}
