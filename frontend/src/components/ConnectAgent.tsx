import { useState } from 'react';
import { Bot, Download, ShieldCheck } from 'lucide-react';
import { api, saveBase64, saveText } from '../lib/api';
import { useStore } from '../lib/store';
import { Modal, Snippet } from './ui';

const clients = ['Claude Code', 'Claude Desktop', 'Codex', 'Cursor', 'VS Code', 'Gemini CLI', 'Windsurf', 'Other'] as const;
type Client = (typeof clients)[number];

/**
 * The `claude mcp add` command for SheetDiver. User scope registers it once
 * for every project, which suits a library you use across boards.
 */
export function claudeAddCommand(url: string, scope: 'user' | 'project' = 'user'): string {
  return `claude mcp add --transport http --scope ${scope} sheetdiver ${url}`;
}

/** A connection snippet; with a file name it can also be saved as that file. */
export type ConnectSnippet = { note: string; text: string; file?: string };

/** Quotes a path for a shell command line when it needs it (spaces). */
function shellPath(p: string): string {
  return /[\s'"&()]/.test(p) ? `"${p}"` : p;
}

/**
 * How to connect each client: to the HTTP endpoint at url, or by running
 * command (this app's own executable) with "mcp" for clients that only
 * launch servers. A bare "sheetdiver" isn't on PATH on Windows and macOS.
 */
export function connectSnippets(url: string, command: string, platform: string): Record<Client, ConnectSnippet[]> {
  const win = platform === 'windows';
  const home = win ? '%USERPROFILE%\\' : '~/';
  const stdio = { command, args: ['mcp'] };
  const json = (v: unknown) => JSON.stringify(v, null, 2) + '\n';
  const desktopConfig = platform === 'darwin' ? '~/Library/Application Support/Claude/claude_desktop_config.json' : win ? '%APPDATA%\\Claude\\claude_desktop_config.json' : 'claude_desktop_config.json';
  return {
    'Claude Code': [
      { note: 'Run once in your terminal. User scope makes your datasheet library available in every project:', text: claudeAddCommand(url) },
      { note: 'Or share it with a hardware repo through .mcp.json (project scope) at the repo root:', text: json({ mcpServers: { sheetdiver: { type: 'http', url } } }), file: '.mcp.json' },
    ],
    'Claude Desktop': [{ note: `Or add it by hand to ${desktopConfig}:`, text: json({ mcpServers: { sheetdiver: stdio } }), file: 'claude_desktop_config.json' }],
    Codex: [
      { note: 'Run once in your terminal:', text: `codex mcp add sheetdiver -- ${shellPath(command)} mcp` },
      // A TOML literal string: Windows paths need no escaping.
      { note: `Or add to ${home}.codex${win ? '\\' : '/'}config.toml:`, text: `[mcp_servers.sheetdiver]\ncommand = '${command}'\nargs = ["mcp"]\n`, file: 'config.toml' },
    ],
    Cursor: [{ note: `Add to ${home}.cursor${win ? '\\' : '/'}mcp.json (or .cursor/mcp.json in a project):`, text: json({ mcpServers: { sheetdiver: { url } } }), file: 'mcp.json' }],
    'VS Code': [{ note: 'Add to .vscode/mcp.json in your workspace:', text: json({ servers: { sheetdiver: { type: 'http', url } } }), file: 'mcp.json' }],
    'Gemini CLI': [
      { note: 'Run once in your terminal:', text: `gemini mcp add --transport http --scope user sheetdiver ${url}` },
      { note: `Or add to ${home}.gemini${win ? '\\' : '/'}settings.json:`, text: json({ mcpServers: { sheetdiver: { httpUrl: url } } }), file: 'settings.json' },
    ],
    Windsurf: [{ note: `Add to ${home}.codeium${win ? '\\' : '/'}windsurf${win ? '\\' : '/'}mcp_config.json:`, text: json({ mcpServers: { sheetdiver: { serverUrl: url } } }), file: 'mcp_config.json' }],
    Other: [
      { note: 'Streamable HTTP endpoint:', text: url },
      { note: 'Or launch over stdio (bridges to the running app, or serves the library itself while it is closed):', text: `${shellPath(command)} mcp` },
    ],
  };
}

/** Dialog with MCP connection snippets and config files for popular agents. */
export function ConnectAgent() {
  const s = useStore();
  const [client, setClient] = useState<Client>('Claude Code');
  const [busy, setBusy] = useState(false);
  const mcp = s.app?.mcp;
  const desktop = !!s.app?.canSaveFile;
  const url = mcp?.url || `http://127.0.0.1:${s.app?.config.settings.mcpPort ?? 47480}/mcp`;
  const snippets = connectSnippets(url, s.app?.command || 'sheetdiver', s.app?.platform ?? '');
  const saved = (title: string, where: string) => where && s.toast({ kind: 'ok', title, body: desktop ? where : `Downloaded ${where}` });
  const saveSnippet = async (sn: ConnectSnippet) => {
    try {
      saved(`Saved ${sn.file}`, await saveText(sn.file!, sn.text, sn.file!.endsWith('.toml') ? 'application/toml' : 'application/json', desktop));
    } catch (e) {
      s.toast({ kind: 'err', title: `Could not save ${sn.file}`, body: (e as Error).message });
    }
  };
  const saveExtension = async () => {
    setBusy(true);
    try {
      const x = await api.mcpExtension();
      saved('Claude Desktop extension saved', await saveBase64(x.name, x.data, 'application/zip', desktop));
    } catch (e) {
      s.toast({ kind: 'err', title: 'Could not save the extension', body: (e as Error).message });
    } finally {
      setBusy(false);
    }
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
      {client === 'Claude Desktop' && (
        <div className="col" style={{ gap: 6 }}>
          <div className="muted">
            Save the SheetDiver extension, then open the saved <span className="mono">sheetdiver.mcpb</span> (or drag it onto Claude Desktop's Settings › Extensions) and
            choose Install. It runs this copy of SheetDiver, so save it again if you move the app.
          </div>
          <button className="btn primary" style={{ alignSelf: 'flex-start' }} disabled={busy} onClick={saveExtension}>
            <Download size={14} /> Save Claude Desktop extension (.mcpb)
          </button>
        </div>
      )}
      {snippets[client].map((sn, i) => (
        <div key={i} className="col" style={{ gap: 6 }}>
          <div className="muted">{sn.note}</div>
          <Snippet text={sn.text} />
          {sn.file && (
            <button className="btn ghost sm" style={{ alignSelf: 'flex-start' }} onClick={() => saveSnippet(sn)} title="If the file already exists, paste the snippet into it instead">
              <Download size={13} /> Save as {sn.file}
            </button>
          )}
        </div>
      ))}
      {snippets[client].some((sn) => sn.file) && (
        <div className="faint">“Save as” writes a new file. If yours already exists, paste the snippet into it instead, so you keep your other servers and settings.</div>
      )}
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
