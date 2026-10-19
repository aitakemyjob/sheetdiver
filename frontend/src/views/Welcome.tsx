import { Bot, Calculator, FileUp, FolderKanban, LineChart, Search } from 'lucide-react';
import { useStore } from '../lib/store';
import { Logo } from '../components/Logo';

/** Shown when no tab is open. */
export function Welcome() {
  const s = useStore();
  const card = (icon: React.ReactNode, title: string, body: string, onClick: () => void) => (
    <div className="card" style={{ cursor: 'pointer' }} onClick={onClick}>
      <div className="row" style={{ marginBottom: 6 }}>
        {icon}
        <b>{title}</b>
      </div>
      <div className="muted">{body}</div>
    </div>
  );
  return (
    <div className="scroll" style={{ flex: 1 }}>
      <div className="pad col" style={{ maxWidth: 920, margin: '0 auto', gap: 18, paddingTop: 48 }}>
        <div className="row" style={{ gap: 14 }}>
          <Logo size={54} />
          <div>
            <h1 style={{ margin: 0, fontSize: 26, letterSpacing: '-0.02em' }}>Dive into your datasheets</h1>
            <div className="muted">Parts, parameters, graphs, schematics, pins and registers: extracted, searchable, and shared with your AI agent.</div>
          </div>
        </div>
        <div className="dropzone" onClick={() => s.setImportOpen(true)}>
          <FileUp size={26} />
          <div style={{ marginTop: 8, fontWeight: 600 }}>Import datasheets</div>
          <div className="faint">Drop PDFs anywhere in the window, choose files, or paste a URL.</div>
        </div>
        <div className="grid2">
          {card(<Bot size={16} style={{ color: 'var(--accent-2)' }} />, 'Connect your agent', 'Claude Code, Cursor, VS Code and others get the whole library over MCP, and can record what they verify.', () => s.setConnectOpen(true))}
          {card(<Search size={16} style={{ color: 'var(--accent)' }} />, 'Search everything', 'Part numbers and orderable codes, parameters, figure captions, pins, registers and notes.', () => s.open({ kind: 'search', title: 'Search', params: { query: '' } }))}
          {card(<FolderKanban size={16} style={{ color: 'var(--accent)' }} />, 'Group parts by board', 'Projects track rails and loads, and check every part against its datasheet limits.', () => s.setSidebar('projects'))}
          {card(<Calculator size={16} style={{ color: 'var(--accent)' }} />, 'Design calculators', 'Buck, boost, LDO, MOSFET gate drive and dividers, filled from the datasheet with page references.', () => s.open({ kind: 'calc', title: 'Buck calculator', params: { kind: 'buck' } }))}
          {card(<LineChart size={16} style={{ color: 'var(--accent)' }} />, 'Graphs and schematics', 'Every captioned figure is cut out and classified; describe what a graph shows to make it searchable.', () => s.setSidebar('library'))}
        </div>
      </div>
    </div>
  );
}
