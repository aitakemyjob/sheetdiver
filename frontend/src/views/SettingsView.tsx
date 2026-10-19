import { useStore } from '../lib/store';
import { themes } from '../lib/themes';
import { Switch } from '../components/ui';

/** Settings: appearance, agent access, server and library. */
export function SettingsView() {
  const s = useStore();
  const st = s.app?.config.settings;
  if (!st) return null;
  const row = (title: string, help: string, control: React.ReactNode) => (
    <div className="row" style={{ padding: '10px 0', borderBottom: '1px solid var(--border)', alignItems: 'flex-start' }}>
      <div className="grow">
        <div style={{ fontWeight: 600 }}>{title}</div>
        <div className="muted" style={{ fontSize: 12 }}>{help}</div>
      </div>
      {control}
    </div>
  );
  return (
    <div className="view">
      <div className="scroll pad col" style={{ flex: 1, maxWidth: 820, gap: 16 }}>
        <div className="card">
          <h3>Appearance</h3>
          {row(
            'Theme',
            'Shared with CodeDiver.',
            <select className="input select" style={{ width: 200 }} value={st.theme} onChange={(e) => s.setTheme(e.target.value)}>
              {themes.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>,
          )}
          {row(
            'Font size',
            'Interface text size in pixels.',
            <input
              className="input"
              type="number"
              min={10}
              max={18}
              style={{ width: 80 }}
              value={st.fontSize}
              onChange={(e) => {
                const v = Number(e.target.value);
                document.documentElement.style.setProperty('--ui-font-size', `${v}px`);
                s.saveSettings({ fontSize: v });
              }}
            />,
          )}
        </div>
        <div className="card">
          <h3>Agent access</h3>
          {row('Allow agents', 'Serve the library over MCP. Off refuses every tool call.', <Switch on={st.mcpEnabled} onChange={(v) => s.saveSettings({ mcpEnabled: v })} />)}
          {row(
            'Allow changes',
            'Let agents record parameters, describe figures, add notes and edit projects. Their changes are marked “agent”.',
            <Switch on={st.agentWrites} onChange={(v) => s.saveSettings({ agentWrites: v })} />,
          )}
          {row('Allow imports', 'Let agents import datasheets from a URL or a local PDF.', <Switch on={st.agentImport} onChange={(v) => s.saveSettings({ agentImport: v })} />)}
          {row('Follow agent', 'Open what the agent shows automatically (otherwise you get a notification).', <Switch on={st.followAgent} onChange={(v) => s.saveSettings({ followAgent: v })} />)}
        </div>
        <div className="card">
          <h3>Server & library</h3>
          {row(
            'MCP port',
            'Loopback port for the MCP endpoint and the browser UI. Takes effect on restart.',
            <input className="input" type="number" style={{ width: 100 }} defaultValue={st.mcpPort} onBlur={(e) => s.saveSettings({ mcpPort: Number(e.target.value) })} />,
          )}
          {row(
            'Library folder',
            `Where the database, PDFs and renders live. Takes effect on restart. Currently ${s.app?.libraryDir}.`,
            <input className="input" style={{ width: 300 }} placeholder="default" defaultValue={st.libraryDir} onBlur={(e) => s.saveSettings({ libraryDir: e.target.value.trim() })} />,
          )}
        </div>
        <div className="faint">SheetDiver {s.app?.version} · MIT licensed</div>
      </div>
    </div>
  );
}
