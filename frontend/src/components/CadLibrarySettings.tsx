import { useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { api } from '../lib/api';
import { cadActions, keeps, settingsFormats, toggleFormat } from '../lib/cad';
import { useStore } from '../lib/store';
import { useAsync } from './ui';

/**
 * Settings for the user's CAD library: the folder SheetDiver keeps it in,
 * its name in the CAD tools, the formats it's kept in, and syncing it now.
 * The card says where each format's files are and how to add them to
 * KiCad.
 */
export function CadLibrarySettings() {
  const s = useStore();
  const st = s.app?.config.settings;
  const key = `${st?.cadLibraryDir}|${st?.cadLibraryName}|${(st?.cadFormats ?? []).join(',')}`;
  const lib = useAsync(() => api.cadLibrary(), [key]);
  const [report, setReport] = useState('');
  // The formats from the settings, so a box ticks as soon as it's clicked.
  const l = lib.data && { ...lib.data, formats: settingsFormats(st?.cadFormats) };
  const sync = () =>
    cadActions
      .sync()
      .then(setReport)
      .catch((e: Error) => setReport(`Could not sync: ${e.message}`));
  return (
    <div className="card">
      <h3>CAD library</h3>
      <div className="muted" style={{ fontSize: 12 }}>
        SheetDiver keeps a symbol, footprint and 3D model for your parts here, drawn from their datasheets by SheetDiver and your agents. Edit them in KiCad as you like: your edits are read back and kept.
      </div>
      <div className="row" style={{ padding: '10px 0' }}>
        <div className="grow" style={{ fontWeight: 600 }}>Folder</div>
        <input
          className="input"
          style={{ width: 360 }}
          aria-label="CAD library folder"
          placeholder={l?.dir ?? 'in the SheetDiver library'}
          defaultValue={st?.cadLibraryDir ?? ''}
          onBlur={(e) => e.target.value.trim() !== (st?.cadLibraryDir ?? '') && s.saveSettings({ cadLibraryDir: e.target.value.trim() })}
        />
      </div>
      <div className="row" style={{ padding: '4px 0 10px' }}>
        <div className="grow" style={{ fontWeight: 600 }}>Library name</div>
        <input
          className="input"
          style={{ width: 200 }}
          aria-label="CAD library name"
          placeholder="SheetDiver"
          defaultValue={st?.cadLibraryName ?? ''}
          onBlur={(e) => e.target.value.trim() !== (st?.cadLibraryName ?? '') && s.saveSettings({ cadLibraryName: e.target.value.trim() })}
        />
      </div>
      {l && (
        <div role="group" aria-label="CAD formats" className="row" style={{ gap: 16, flexWrap: 'wrap' }}>
          {l.available.map((f) => (
            <label key={f.name} className="row" style={{ gap: 6 }} title={f.readsEdits ? 'Your edits in it are read back and kept' : 'Written only: edit in KiCad'}>
              <input type="checkbox" checked={keeps(l, f.name)} onChange={(e) => s.saveSettings({ cadFormats: toggleFormat(l, f.name, e.target.checked) })} />
              {f.label}
            </label>
          ))}
          <span className="grow" />
          <button className="btn sm" onClick={sync}>
            <RefreshCw size={13} /> Sync now
          </button>
        </div>
      )}
      {l && keeps(l, 'kicad') && (
        <div className="muted" style={{ fontSize: 12, marginTop: 8 }}>
          In KiCad, add <code>{l.kicad.symbols}</code> under Preferences → Manage Symbol Libraries and <code>{l.kicad.footprints}</code> under Manage Footprint Libraries, named <b>{l.name}</b>.
        </div>
      )}
      {l && keeps(l, 'eagle') && (
        <div className="muted" style={{ fontSize: 12, marginTop: 4 }}>
          Eagle / Fusion library: <code>{l.eagle}</code>.
        </div>
      )}
      {l && keeps(l, 'altium') && (
        <div className="muted" style={{ fontSize: 12, marginTop: 4 }}>
          Altium libraries: <code>{l.name}.SchLib</code> and <code>{l.name}.PcbLib</code> in <code>{l.altium}</code> (each symbol linked to its footprint; 3D models are in the KiCad folder as STEP).
        </div>
      )}
      {report && (
        <div role="status" style={{ fontSize: 12, marginTop: 6 }}>
          {report}
        </div>
      )}
    </div>
  );
}
