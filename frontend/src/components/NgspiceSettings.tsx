import { CheckCircle2, AlertTriangle } from 'lucide-react';
import { api } from '../lib/api';
import { useStore } from '../lib/store';
import { useAsync } from './ui';

/**
 * Settings for simulations: which ngspice they run with. Empty finds it
 * on the PATH or where its installers put it; the card says which one
 * was found and its version, or how to install it.
 */
export function NgspiceSettings() {
  const s = useStore();
  const path = s.app?.config.settings.ngspicePath ?? '';
  const status = useAsync(() => api.ngspice(), [path]);
  const st = status.data;
  return (
    <div className="card">
      <h3>Simulation</h3>
      <div className="row" style={{ padding: '10px 0', alignItems: 'flex-start' }}>
        <div className="grow">
          <div style={{ fontWeight: 600 }}>ngspice</div>
          <div className="muted" style={{ fontSize: 12 }}>
            The ngspice program agents' simulations run with. Leave empty to use the one on your PATH or in its usual place (Windows: C:\Spice64\bin\ngspice_con.exe; macOS: brew install ngspice; Linux: your distribution's ngspice package).
          </div>
        </div>
        <input
          className="input"
          style={{ width: 300 }}
          aria-label="ngspice path"
          placeholder="find it automatically"
          defaultValue={path}
          onBlur={(e) => e.target.value.trim() !== path && s.saveSettings({ ngspicePath: e.target.value.trim() })}
        />
      </div>
      <div role="status" style={{ fontSize: 12 }}>
        {status.loading && !st && <span className="muted">Looking for ngspice…</span>}
        {st?.version && (
          <span style={{ color: 'var(--ok)' }}>
            <CheckCircle2 size={12} /> ngspice {st.version} at {st.path} ({st.from === 'settings' ? 'as set' : st.from === 'PATH' ? 'on the PATH' : 'its usual place'})
          </span>
        )}
        {st && !st.version && (
          <span className="err-text">
            <AlertTriangle size={12} /> {st.error}
          </span>
        )}
      </div>
    </div>
  );
}
