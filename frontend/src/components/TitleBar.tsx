import { Bot, FileUp, FolderKanban, Palette, Search } from 'lucide-react';
import { useStore } from '../lib/store';
import { themes } from '../lib/themes';
import { Menu } from './Menu';
import { Logo } from './Logo';

/**
 * Top bar: logo, active project, search, import, follow-agent toggle, MCP
 * status and theme menu.
 */
export function TitleBar() {
  const s = useStore();
  const isMac = s.app?.platform === 'darwin';
  const mod = isMac ? '⌘' : 'Ctrl';
  const mcp = s.app?.mcp;
  const follow = s.app?.config.settings.followAgent ?? true;
  const active = s.app?.config.activeProject;

  return (
    <header className={`titlebar ${isMac ? 'mac' : ''}`}>
      <div className="brand">
        <Logo />
        <span>
          <span className="sheet">Sheet</span>
          <span className="diver">Diver</span>
        </span>
      </div>

      {active && (
        <button className="btn ghost" title="Active project" onClick={() => s.open({ kind: 'project', title: active, params: { project: active } })}>
          <FolderKanban size={15} className="faint" />
          <span style={{ fontWeight: 600 }}>{active}</span>
        </button>
      )}

      <div className="spacer" />

      <div className="omnibox" onClick={() => s.open({ kind: 'search', title: 'Search', params: { query: '' } })}>
        <Search size={14} />
        <span>Search parts, parameters, figures, pins, notes…</span>
        <span className="kbd">{mod} K</span>
      </div>

      <div className="spacer" />

      <button className="btn" onClick={() => s.setImportOpen(true)} title="Import datasheets">
        <FileUp size={14} /> Import
      </button>

      <button
        className={`pill ${follow ? '' : 'faint'}`}
        title={follow ? 'Following the agent: what it shows opens automatically' : 'Not following the agent'}
        onClick={() => s.saveSettings({ followAgent: !follow })}
      >
        <Bot size={14} style={{ color: follow ? 'var(--accent-2)' : undefined }} />
        {follow ? 'Following agent' : 'Follow agent'}
      </button>

      <span className="pill" onClick={() => s.setConnectOpen(true)} title={mcp?.error || mcp?.url || 'MCP server'}>
        <span className={`dot ${mcp?.running ? 'ok' : mcp?.error ? 'err' : 'idle'}`} />
        MCP {mcp?.running ? 'live' : mcp?.error ? 'error' : 'off'}
      </span>

      <Menu
        align="right"
        width={220}
        trigger={() => (
          <button className="btn ghost icon" title="Theme">
            <Palette size={16} />
          </button>
        )}
        items={themes.map((t) => ({
          key: t.id,
          label: t.name,
          active: t.id === s.theme.id,
          icon: <span style={{ width: 14, height: 14, borderRadius: 4, background: t.accent, display: 'block' }} />,
          hint: t.mode,
          onSelect: () => s.setTheme(t.id),
        }))}
      />
    </header>
  );
}
