import { AlertCircle, Bot, Database, Plug } from 'lucide-react';
import { useStore } from '../lib/store';
import { relTime } from '../lib/format';
import { queueCounts } from './ImportQueue';

/** Bottom bar: library folder, import progress, last agent action and the MCP endpoint. */
export function StatusBar() {
  const s = useStore();
  const last = s.activity[s.activity.length - 1];
  const mcp = s.app?.mcp;
  const c = queueCounts(Object.values(s.jobs));
  return (
    <footer className="statusbar">
      <span className="item" title={s.app?.libraryDir}>
        <Database size={12} />
        <span className="mono ellipsis" style={{ maxWidth: 360 }}>{s.app?.libraryDir}</span>
      </span>
      {c.active > 0 && (
        <span className="item click" onClick={() => s.setQueueOpen(true)} title="Show the import queue">
          <div className="spinner" style={{ width: 10, height: 10, borderWidth: 1.5 }} />
          Importing {c.active} · {c.done + c.existing} done
        </span>
      )}
      {c.active === 0 && c.failed > 0 && (
        <span className="item click" onClick={() => s.setQueueOpen(true)} title="Show the import queue">
          <AlertCircle size={12} style={{ color: 'var(--err)' }} />
          {c.failed} import{c.failed === 1 ? '' : 's'} failed
        </span>
      )}
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
