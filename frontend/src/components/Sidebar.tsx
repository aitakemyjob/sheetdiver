import { useMemo, useState } from 'react';
import { Bot, CloudDownload, FileUp, FolderKanban, ListChecks, Plus, Star } from 'lucide-react';
import { api } from '../lib/api';
import { categories, plural, relTime } from '../lib/format';
import { finished, useActiveTab, useStore } from '../lib/store';
import { jobProgress } from './ImportQueue';
import { AllUpdatesDialog } from './UpdateDialog';
import { FavoriteStar, StatusBadge } from './PartStatus';
import { CategoryBadge, ErrorBox, useAsync } from './ui';

/** The sidebar: library, projects or agent activity. */
export function Sidebar() {
  const mode = useStore((s) => s.sidebar);
  return (
    <aside className="sidebar">
      {mode === 'library' && <LibraryPanel />}
      {mode === 'types' && <TypesPanel />}
      {mode === 'projects' && <ProjectsPanel />}
      {mode === 'agent' && <AgentPanel />}
    </aside>
  );
}

/** Parts in the library, filtered by text and category. */
function LibraryPanel() {
  const s = useStore();
  const tab = useActiveTab();
  const [q, setQ] = useState('');
  const [cat, setCat] = useState('');
  const [fav, setFav] = useState(false);
  const [updates, setUpdates] = useState(false);
  const parts = useAsync(() => api.parts({ query: q, category: cat, favorite: fav }), [q, cat, fav, s.libraryVersion]);
  const cats = useAsync(() => api.categories(), [s.libraryVersion]);
  const activePart = tab?.kind === 'part' ? String(tab.params.part ?? '').toUpperCase() : '';
  const catList = useMemo(() => Object.entries(cats.data ?? {}).filter(([c]) => c).sort((a, b) => b[1] - a[1]), [cats.data]);
  const active = Object.values(s.jobs).filter((j) => !finished(j));
  const running = active.filter((j) => j.status === 'running');
  return (
    <>
      <div className="panel-head">
        Library
        <span className="faint" style={{ textTransform: 'none', letterSpacing: 0, fontWeight: 500 }}>
          {parts.data ? plural(parts.data.length, 'part') : ''}
        </span>
        <div className="actions">
          <button className={`btn ghost icon sm ${fav ? 'on' : ''}`} title={fav ? 'Show all parts' : 'Show favorites only'} aria-pressed={fav} onClick={() => setFav(!fav)}>
            <Star size={14} fill={fav ? 'currentColor' : 'none'} style={fav ? { color: 'var(--warn)' } : undefined} />
          </button>
          <button className="btn ghost icon sm" title="Check every datasheet for a newer revision" onClick={() => setUpdates(true)}>
            <CloudDownload size={14} />
          </button>
          <button className="btn ghost icon sm" title="Import queue" onClick={() => s.setQueueOpen(true)}>
            <ListChecks size={14} />
          </button>
          <button className="btn ghost icon sm" title="Import datasheets" onClick={() => s.setImportOpen(true)}>
            <FileUp size={14} />
          </button>
        </div>
      </div>
      <div className="side-search">
        <input className="input" placeholder="Filter by part number or description" value={q} onChange={(e) => setQ(e.target.value)} />
      </div>
      {catList.length > 1 && (
        <div className="chips">
          {catList.map(([c, n]) => (
            <button key={c} className={`chip ${cat === c ? 'on' : ''}`} onClick={() => setCat(cat === c ? '' : c)}>
              {categories[c] ?? c} <span className="faint">{n}</span>
            </button>
          ))}
        </div>
      )}
      {active.length === 1 && (
        <div className="import-progress click" onClick={() => s.setQueueOpen(true)}>
          <div className="ellipsis">
            Importing <b>{active[0].name}</b>
          </div>
          <div className="faint">{jobProgress(active[0])}</div>
          <div className="bar">
            <i style={{ width: `${active[0].total ? (100 * active[0].done) / active[0].total : 10}%` }} />
          </div>
        </div>
      )}
      {active.length > 1 && (
        <div className="import-progress click" onClick={() => s.setQueueOpen(true)} title="Show the import queue">
          <div>
            Importing <b>{active.length}</b> datasheets
          </div>
          <div className="faint ellipsis">{running.map((j) => j.name).join(', ') || 'queued'}</div>
        </div>
      )}
      {updates && <AllUpdatesDialog onClose={() => setUpdates(false)} />}
      <div className="panel-body">
        <ErrorBox error={parts.error} />
        {parts.data?.length === 0 && fav && <div className="faint" style={{ padding: 10 }}>No favorites yet: star a part to keep it here.</div>}
        {parts.data?.length === 0 && !q && !cat && !fav && (
          <div className="dropzone" style={{ margin: 8 }} onClick={() => s.setImportOpen(true)}>
            <FileUp size={22} />
            <div style={{ marginTop: 8 }}>Drop datasheet PDFs here or click to import</div>
          </div>
        )}
        {parts.data?.map((p) => (
          <div key={p.id} className={`part-row ${activePart === p.mpn.toUpperCase() ? 'on' : ''}`} onClick={() => s.open({ kind: 'part', title: p.mpn, params: { part: p.mpn } })}>
            <div className="mpn">
              {p.mpn}
              <CategoryBadge category={p.category} label={categories[p.category]} />
              <StatusBadge status={p.status} />
              <div className="grow" />
              <FavoriteStar part={p} size={13} />
            </div>
            <div className="desc" title={p.description}>
              {p.manufacturer ? `${p.manufacturer} · ` : ''}
              {p.description}
            </div>
          </div>
        ))}
      </div>
    </>
  );
}

/** Component types; each opens a comparison of its parts. */
function TypesPanel() {
  const s = useStore();
  const tab = useActiveTab();
  const list = useAsync(() => api.categoryList(), [s.libraryVersion]);
  const keys = s.app?.keys ?? {};
  return (
    <>
      <div className="panel-head">Component types</div>
      <div className="panel-body">
        <ErrorBox error={list.error} />
        {list.data?.length === 0 && <div className="faint" style={{ padding: 10 }}>Import datasheets to see their component types here.</div>}
        {list.data?.map((c) => (
          <div
            key={c.category}
            className={`part-row ${tab?.kind === 'category' && tab.params.category === c.category ? 'on' : ''}`}
            onClick={() => s.open({ kind: 'category', title: categories[c.category] ?? (c.category || 'Uncategorized'), params: { category: c.category } })}
            title={(c.keys ?? []).map((k) => `${k}: ${keys[k] ?? ''}`).join('\n')}
          >
            <div className="mpn" style={{ fontFamily: 'var(--font-ui)' }}>
              <CategoryBadge category={c.category || 'other'} label={categories[c.category] ?? (c.category || 'Uncategorized')} />
              <span className="faint" style={{ marginLeft: 'auto', fontWeight: 500 }}>
                {c.count} {c.count === 1 ? 'part' : 'parts'}
              </span>
            </div>
            <div className="desc">{c.keys?.length ? `Compare ${c.keys.slice(0, 6).join(', ')}${c.keys.length > 6 ? '…' : ''}` : 'Compare by every parameter key'}</div>
          </div>
        ))}
      </div>
    </>
  );
}

/** Projects with their part counts. */
function ProjectsPanel() {
  const s = useStore();
  const tab = useActiveTab();
  const list = useAsync(() => api.projects(), [s.projectVersion]);
  const [name, setName] = useState('');
  const active = s.app?.config.activeProject ?? '';
  const create = async () => {
    if (!name.trim()) return;
    try {
      const p = await api.createProject(name.trim());
      if (!active) await api.setActiveProject(p.name);
      setName('');
      s.open({ kind: 'project', title: p.name, params: { project: p.name } });
      useStore.setState({ projectVersion: s.projectVersion + 1 });
    } catch (e) {
      s.toast({ kind: 'err', title: 'Could not create project', body: (e as Error).message });
    }
  };
  return (
    <>
      <div className="panel-head">Projects</div>
      <div className="side-search row">
        <input className="input" placeholder="New project name" value={name} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && create()} />
        <button className="btn icon" onClick={create} title="Create project">
          <Plus size={14} />
        </button>
      </div>
      <div className="panel-body">
        <ErrorBox error={list.error} />
        {list.data?.length === 0 && <div className="faint" style={{ padding: 10 }}>Group the parts of a board into a project to see its power budget, rail checks and saved calculations.</div>}
        {list.data?.map((p) => (
          <div key={p.id} className={`part-row ${tab?.kind === 'project' && String(tab.params.project).toLowerCase() === p.name.toLowerCase() ? 'on' : ''}`} onClick={() => s.open({ kind: 'project', title: p.name, params: { project: p.name } })}>
            <div className="mpn" style={{ fontFamily: 'var(--font-ui)' }}>
              <FolderKanban size={13} className="faint" />
              {p.name}
              <button
                className="btn ghost icon sm"
                style={{ marginLeft: 'auto', color: p.name === active ? 'var(--accent-2)' : undefined }}
                title={p.name === active ? 'Active project (agents default to it)' : 'Make active'}
                onClick={async (e) => {
                  e.stopPropagation();
                  await api.setActiveProject(p.name === active ? '' : p.name);
                  const app = await api.state();
                  useStore.setState({ app });
                }}
              >
                <Star size={13} fill={p.name === active ? 'currentColor' : 'none'} />
              </button>
            </div>
            <div className="desc">
              {plural(p.parts, 'part')} · {plural(p.rails, 'rail')} · {relTime(p.updatedAt)}
            </div>
          </div>
        ))}
      </div>
    </>
  );
}

/** What the agent has been doing. */
function AgentPanel() {
  const s = useStore();
  const items = [...s.activity].reverse();
  return (
    <>
      <div className="panel-head">Agent activity</div>
      <div className="panel-body">
        {items.length === 0 && (
          <div className="faint" style={{ padding: 10 }}>
            <Bot size={16} /> Nothing yet. Connect an agent from the MCP pill in the title bar.
          </div>
        )}
        {items.map((a) => (
          <div key={a.id} className={`feed-item ${a.error ? 'err' : ''}`} title={a.error || a.summary}>
            <div className="grow" style={{ minWidth: 0 }}>
              <div className="tool">{a.tool}</div>
              <div className="ellipsis faint">{a.error || a.summary}</div>
            </div>
            <span className="when">{relTime(a.time)}</span>
          </div>
        ))}
      </div>
    </>
  );
}
