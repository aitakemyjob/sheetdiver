import { Bot, Database, Plug } from 'lucide-react';
import { useStore } from '../lib/store';
import { relTime } from '../lib/format';

/** Bottom bar: library folder, import progress, last agent action and the MCP endpoint. */
export function StatusBar() {
  const s = useStore();
  const last = s.activity[s.activity.length - 1];
  const mcp = s.app?.mcp;
  const imports = Object.values(s.imports);
  return (
    <footer className="statusbar">
      <span className="item" title={s.app?.libraryDir}>
        <Database size={12} />
        <span className="mono ellipsis" style={{ maxWidth: 360 }}>{s.app?.libraryDir}</span>
      </span>
      {imports.map((p) => (
        <span className="item" key={p.filename}>
          <div className="spinner" style={{ width: 10, height: 10, borderWidth: 1.5 }} />
          {p.filename}: {p.stage} {p.total ? `${p.done}/${p.total}` : ''}
        </span>
      ))}
      <div className="right">
        {last && (
          <span className="item click" onClick={() => s.setSidebar('agent')} title={last.summary}>
            <Bot size={12} style={{ color: 'var(--accent-2)' }} />
            <span className="mono">{last.tool}</span> {relTime(last.time)}
          </span>
        )}
        <span className="item click" onClick={() => s.setConnectOpen(true)} title={mcp?.error ?? 'Agent endpoint'}>
          <Plug size={12} />
          {mcp?.running ? <span className="mono">{mcp.url}</span> : mcp?.error ? <span style={{ color: 'var(--err)' }}>MCP error</span> : 'MCP off'}
        </span>
        <span className="item">{s.app?.version?.startsWith('v') ? '' : 'v'}{s.app?.version}</span>
      </div>
    </footer>
  );
}
