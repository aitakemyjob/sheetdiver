import { useActiveTab, useStore } from '../lib/store';
import { CalcView } from './CalcView';
import { CategoryView } from './CategoryView';
import { FigureView } from './FigureView';
import { PageView } from './PageView';
import { PartView } from './PartView';
import { ProjectView } from './ProjectView';
import { SearchView } from './SearchView';
import { SettingsView } from './SettingsView';
import { Welcome } from './Welcome';

/** Renders the active tab (every open tab stays mounted for its state). */
export function ViewHost() {
  const tabs = useStore((s) => s.tabs);
  const active = useActiveTab();
  if (!active) return <Welcome />;
  return (
    <>
      {tabs.map((t) => (
        <div key={t.id} className="view" style={{ display: t.id === active.id ? 'flex' : 'none' }}>
          {t.kind === 'part' && <PartView params={t.params} />}
          {t.kind === 'page' && <PageView params={t.params} />}
          {t.kind === 'figure' && <FigureView params={t.params} />}
          {t.kind === 'project' && <ProjectView params={t.params} />}
          {t.kind === 'calc' && <CalcView params={t.params} />}
          {t.kind === 'search' && <SearchView params={t.params} />}
          {t.kind === 'settings' && <SettingsView />}
          {t.kind === 'category' && <CategoryView params={t.params} />}
        </div>
      ))}
    </>
  );
}
