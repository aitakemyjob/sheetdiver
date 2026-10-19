import { Bot, Calculator, Database, FolderKanban, LayoutGrid, Library, Search, Settings } from 'lucide-react';
import { useStore, type SidebarMode } from '../lib/store';

/** Left icon rail: sidebar panels, search, calculators and settings. */
export function ActivityBar() {
  const s = useStore();
  const btn = (mode: SidebarMode, icon: React.ReactNode, title: string, count?: number) => (
    <button className={`ab-btn ${s.sidebar === mode && s.sidebarOpen ? 'on' : ''}`} title={title} onClick={() => s.setSidebar(mode)}>
      {icon}
      {count ? <span className="count">{count > 99 ? '99+' : count}</span> : null}
    </button>
  );
  const recent = s.activity.filter((a) => Date.now() - new Date(a.time).getTime() < 60_000).length;
  return (
    <nav className="activitybar">
      {btn('library', <Library size={19} />, 'Library')}
      {btn('types', <LayoutGrid size={19} />, 'Component types')}
      {btn('projects', <FolderKanban size={19} />, 'Projects')}
      {btn('agent', <Bot size={19} />, 'Agent activity', recent)}
      <button className={`ab-btn ${s.catalog?.stale ? 'dot' : ''}`} title={s.catalog?.stale ? `Parts catalogue (${s.catalog.ageDays} days old)` : 'Parts catalogue (JLCPCB)'} onClick={() => s.open({ kind: 'catalog', title: 'Parts catalogue', params: {} })}>
        <Database size={19} />
      </button>
      <button className="ab-btn" title="Search" onClick={() => s.open({ kind: 'search', title: 'Search', params: { query: '' } })}>
        <Search size={19} />
      </button>
      <button className="ab-btn" title="Calculators" onClick={() => s.open({ kind: 'calc', title: 'Buck calculator', params: { kind: 'buck' } })}>
        <Calculator size={19} />
      </button>
      <div className="bottom">
        <button className="ab-btn" title="Settings" onClick={() => s.open({ kind: 'settings', title: 'Settings', params: {} })}>
          <Settings size={19} />
        </button>
      </div>
    </nav>
  );
}
