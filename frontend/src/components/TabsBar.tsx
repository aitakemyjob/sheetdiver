import { Calculator, Cpu, FileText, FolderKanban, LineChart, Search, Settings, X } from 'lucide-react';
import { useStore } from '../lib/store';
import type { TabKind } from '../lib/tabs';

const icons: Record<TabKind, typeof Cpu> = {
  part: Cpu, page: FileText, figure: LineChart, project: FolderKanban, calc: Calculator, search: Search, settings: Settings,
};

/**
 * Tab strip. Middle-click closes, and agent-opened tabs show a dot until
 * viewed.
 */
export function TabsBar() {
  const s = useStore();
  return (
    <div className="tabs" role="tablist">
      {s.tabs.map((t) => {
        const Icon = icons[t.kind];
        return (
          <div
            key={t.id}
            role="tab"
            aria-selected={t.id === s.active}
            className={`tab ${t.id === s.active ? 'on' : ''}`}
            onClick={() => s.activate(t.id)}
            onAuxClick={(e) => e.button === 1 && s.close(t.id)}
            title={t.title}
          >
            {t.agent && t.id !== s.active ? <span className="agent-dot" /> : <Icon size={14} style={{ flex: 'none', opacity: 0.8 }} />}
            <span className="ellipsis">{t.title}</span>
            <button
              className="x"
              onClick={(e) => {
                e.stopPropagation();
                s.close(t.id);
              }}
              aria-label="Close tab"
            >
              <X size={12} />
            </button>
          </div>
        );
      })}
    </div>
  );
}
