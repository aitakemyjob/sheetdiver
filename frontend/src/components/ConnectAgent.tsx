import { useState } from 'react';
import { Bot, ShieldCheck } from 'lucide-react';
import { useStore } from '../lib/store';
import { Modal, Snippet } from './ui';

const clients = ['Claude Code', 'Cursor', 'VS Code', 'Claude Desktop', 'Codex', 'Other'] as const;

/**
 * The `claude mcp add` command for SheetDiver. User scope registers it once
 * for every project, which suits a library you use across boards.
 */
export function claudeAddCommand(url: string, scope: 'user' | 'project' = 'user'): string {
  return `claude mcp add --transport http --scope ${scope} sheetdiver ${url}`;
}

/** Dialog with MCP connection snippets for popular agents. */
export function ConnectAgent() {
  const s = useStore();
  const [client, setClient] = useState<(typeof clients)[number]>('Claude Code');
  const mcp = s.app?.mcp;
  const url = mcp?.url || `http://127.0.0.1:${s.app?.config.settings.mcpPort ?? 47480}/mcp`;
  const snippets: Record<(typeof clients)[number], { note: string; text: string }[]> = {
    'Claude Code': [
      { note: 'Run once in your terminal. User scope makes your datasheet library available in every project:', text: claudeAddCommand(url) },
      { note: 'Or share it with a hardware repo through .mcp.json (project scope):', text: claudeAddCommand(url, 'project') },
    ],
    Cursor: [{ note: 'Add to ~/.cursor/mcp.json:', text: JSON.stringify({ mcpServers: { sheetdiver: { url } } }, null, 2) }],
    'VS Code': [{ note: 'Add to .vscode/mcp.json:', text: JSON.stringify({ servers: { sheetdiver: { type: 'http', url } } }, null, 2) }],
    'Claude Desktop': [
      { note: 'Claude Desktop speaks stdio; SheetDiver bridges it to this window. Add to claude_desktop_config.json:', text: JSON.stringify({ mcpServers: { sheetdiver: { command: 'sheetdiver', args: ['mcp'] } } }, null, 2) },
    ],
    Codex: [{ note: 'Add to ~/.codex/config.toml:', text: `[mcp_servers.sheetdiver]\ncommand = "sheetdiver"\nargs = ["mcp"]` }],
    Other: [
      { note: 'Streamable HTTP endpoint:', text: url },
      { note: 'Or launch over stdio (bridges to the running app):', text: 'sheetdiver mcp' },
    ],
  };
  return (
    <Modal title="Connect an agent" icon={<Bot size={16} style={{ color: 'var(--accent-2)' }} />} onClose={() => s.setConnectOpen(false)} wide>
      <div className="row">
        <span className={`dot ${mcp?.running ? 'ok' : 'err'}`} />
        <div className="grow">
          {mcp?.running ? (
            <>
              MCP server is live at <span className="mono">{url}</span>
            </>
          ) : (
            <span style={{ color: 'var(--err)' }}>{mcp?.error || 'MCP server is not running.'} Change the port in Settings and restart SheetDiver.</span>
          )}
        </div>
      </div>
      <div className="seg" style={{ alignSelf: 'flex-start', flexWrap: 'wrap' }}>
        {clients.map((c) => (
          <button key={c} className={c === client ? 'on' : ''} onClick={() => setClient(c)}>
            {c}
          </button>
        ))}
      </div>
      {snippets[client].map((sn, i) => (
        <div key={i} className="col" style={{ gap: 6 }}>
          <div className="muted">{sn.note}</div>
          <Snippet text={sn.text} />
        </div>
      ))}
      <div className="card">
        <h3>Try asking your agent</h3>
        <ul style={{ margin: 0, paddingLeft: 18, lineHeight: 1.8 }} className="muted">
          <li>“Import the TPS563200 datasheet and check the extracted parameters against the electrical characteristics.”</li>
          <li>“Look at the efficiency graphs for the TPS563200 and describe them in SheetDiver.”</li>
          <li>“Design a 12 V → 3.3 V, 2 A buck with the TPS563200: inductor, caps and feedback resistors.”</li>
          <li>“Add the parts of my sensor board to a project and check every rail against the datasheets.”</li>
          <li>“Pick gate resistors for the CSD17578Q3A driven from 5 V at 300 kHz.”</li>
        </ul>
      </div>
      <div className="row muted" style={{ alignItems: 'flex-start' }}>
        <ShieldCheck size={16} style={{ color: 'var(--ok)', flex: 'none', marginTop: 2 }} />
        <span>
          The server only listens on 127.0.0.1. Agents read your library and can record parameters, figure descriptions and notes (marked as “agent” so you can tell
          them apart). Switch agent changes or imports off in Settings.
        </span>
      </div>
    </Modal>
  );
}
