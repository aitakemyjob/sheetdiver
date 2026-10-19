// Global UI state (zustand): app state, tabs, sidebar, toasts, agent
// activity and imports. Backend events arrive through the transport and
// are folded in by handleEvent.
import { create } from 'zustand';
import { api, uploadPDF } from './api';
import { getTransport } from './transport';
import { closeTab, focusToTab, openTab, type Tab } from './tabs';
import { applyTheme, themeById, type Theme } from './themes';
import type { Activity, AppState, BusEvent, Config, Focus, ImportJob, ImportResult, MCPStatus, Part, Settings } from './types';

export type SidebarMode = 'library' | 'types' | 'projects' | 'agent';

export interface Toast {
  id: number;
  kind: 'info' | 'ok' | 'warn' | 'err' | 'agent';
  title: string;
  body?: string;
  action?: { label: string; run: () => void };
}

interface State {
  ready: boolean;
  fatal?: string;
  app?: AppState;
  theme: Theme;
  tabs: Tab[];
  active?: string;
  sidebar: SidebarMode;
  sidebarOpen: boolean;
  toasts: Toast[];
  activity: Activity[];
  connectOpen: boolean;
  importOpen: boolean;
  queueOpen: boolean;
  /** The import queue by job id. */
  jobs: Record<number, ImportJob>;
  /** Bumped when library data changes, so views refetch. */
  libraryVersion: number;
  projectVersion: number;

  init: () => Promise<void>;
  open: (t: Omit<Tab, 'id'>) => void;
  close: (id: string) => void;
  activate: (id: string) => void;
  setSidebar: (m: SidebarMode) => void;
  toggleSidebar: () => void;
  toast: (t: Omit<Toast, 'id'>) => void;
  dismissToast: (id: number) => void;
  setTheme: (id: string) => void;
  saveSettings: (patch: Partial<Settings>) => Promise<void>;
  setConnectOpen: (v: boolean) => void;
  setImportOpen: (v: boolean) => void;
  setQueueOpen: (v: boolean) => void;
  importFiles: (files: File[], opts?: { mpns?: string; manufacturer?: string; category?: string }) => Promise<void>;
  importPaths: (paths: string[], opts?: { mpns?: string[]; manufacturer?: string; category?: string; url?: boolean }) => Promise<void>;
  /** Queues many imports at once; progress arrives as import events. */
  queueImports: (req: { paths?: string[]; urls?: string[]; mpns?: string[]; folder?: string; recursive?: boolean }) => Promise<void>;
  setFavorite: (p: Part, on: boolean) => Promise<void>;
  setStatus: (p: Part, status: string) => Promise<void>;
  handleEvent: (ev: BusEvent) => void;
  refreshLibrary: () => void;
}

let toastSeq = 0;

export const useStore = create<State>((set, get) => ({
  ready: false,
  theme: themeById('deepdive'),
  tabs: [],
  sidebar: 'library',
  sidebarOpen: true,
  toasts: [],
  activity: [],
  connectOpen: false,
  importOpen: false,
  queueOpen: false,
  jobs: {},
  libraryVersion: 0,
  projectVersion: 0,

  async init() {
    try {
      const app = await api.state();
      const theme = themeById(app.config.settings.theme);
      applyTheme(theme);
      document.documentElement.style.setProperty('--ui-font-size', `${app.config.settings.fontSize}px`);
      const activity = await api.activity().catch(() => []);
      const jobs = Object.fromEntries((await api.importJobs().catch(() => [] as ImportJob[])).map((j) => [j.id, j]));
      set({ app, theme, ready: true, activity, jobs });
      const t = await getTransport();
      t.subscribe((ev) => get().handleEvent(ev));
    } catch (e) {
      set({ ready: true, fatal: (e as Error).message });
    }
  },

  open(t) {
    const { tabs, active } = openTab(get().tabs, t);
    set({ tabs, active });
  },
  close(id) {
    const r = closeTab(get().tabs, id, get().active);
    set({ tabs: r.tabs, active: r.active });
  },
  activate(id) {
    set({ active: id, tabs: get().tabs.map((t) => (t.id === id ? { ...t, agent: false } : t)) });
  },
  setSidebar(m) {
    set({ sidebar: m, sidebarOpen: get().sidebar === m ? !get().sidebarOpen : true });
  },
  toggleSidebar() {
    set({ sidebarOpen: !get().sidebarOpen });
  },
  toast(t) {
    const id = ++toastSeq;
    set({ toasts: [...get().toasts, { ...t, id }].slice(-5) });
    setTimeout(() => get().dismissToast(id), t.action ? 9000 : 5000);
  },
  dismissToast(id) {
    set({ toasts: get().toasts.filter((t) => t.id !== id) });
  },
  setTheme(id) {
    const theme = themeById(id);
    applyTheme(theme);
    set({ theme });
    get().saveSettings({ theme: id });
  },
  async saveSettings(patch) {
    const app = get().app;
    if (!app) return;
    const settings = { ...app.config.settings, ...patch };
    set({ app: { ...app, config: { ...app.config, settings } } });
    await api.saveSettings(settings).catch((e) => get().toast({ kind: 'err', title: 'Could not save settings', body: String(e.message ?? e) }));
  },
  setConnectOpen(v) {
    set({ connectOpen: v });
  },
  setImportOpen(v) {
    set({ importOpen: v });
  },
  setQueueOpen(v) {
    set({ queueOpen: v });
  },

  async importFiles(files, opts) {
    const pdfs = files.filter((f) => /\.pdf$/i.test(f.name));
    for (const f of files) if (!pdfs.includes(f)) get().toast({ kind: 'warn', title: `${f.name} is not a PDF` });
    if (pdfs.length > 1) set({ queueOpen: true });
    // Upload a few at a time; the backend extracts them in parallel.
    const one = async (f: File) => {
      try {
        const r = await uploadPDF(f, opts);
        if (pdfs.length === 1) announce(r);
      } catch (e) {
        if (pdfs.length === 1) get().toast({ kind: 'err', title: `Could not import ${f.name}`, body: (e as Error).message });
      }
    };
    await pool(pdfs, 3, one);
  },
  async importPaths(paths, opts = {}) {
    // Several paths without overrides go through the queue.
    if (paths.length > 1 && !opts.mpns && !opts.manufacturer && !opts.category) {
      return get().queueImports(opts.url ? { urls: paths } : { paths });
    }
    for (const p of paths) {
      try {
        const req = opts.url ? { url: p } : { path: p };
        announce(await api.importPath({ ...req, mpns: opts.mpns, manufacturer: opts.manufacturer, category: opts.category }));
      } catch (e) {
        get().toast({ kind: 'err', title: `Could not import ${p.split(/[\\/]/).pop()}`, body: (e as Error).message });
      }
    }
  },
  async queueImports(req) {
    try {
      const jobs = await api.startImports(req);
      set({ queueOpen: true });
      get().toast({ kind: 'info', title: `Importing ${jobs.length} datasheet${jobs.length === 1 ? '' : 's'}`, body: 'Follow them in the import queue.' });
    } catch (e) {
      get().toast({ kind: 'err', title: 'Could not start the imports', body: (e as Error).message });
    }
  },
  async setFavorite(p, on) {
    try {
      await api.updatePart(p.id, { favorite: on });
      get().refreshLibrary();
    } catch (e) {
      get().toast({ kind: 'err', title: 'Could not update the part', body: (e as Error).message });
    }
  },
  async setStatus(p, status) {
    try {
      await api.updatePart(p.id, { status });
      get().refreshLibrary();
    } catch (e) {
      get().toast({ kind: 'err', title: 'Could not update the part', body: (e as Error).message });
    }
  },

  handleEvent(ev) {
    const s = get();
    switch (ev.type) {
      case 'config': {
        const config = ev.data as Config;
        if (s.app) set({ app: { ...s.app, config } });
        break;
      }
      case 'mcp':
        if (s.app) set({ app: { ...s.app, mcp: ev.data as MCPStatus } });
        break;
      case 'activity':
        set({ activity: [...s.activity, ev.data as Activity].slice(-300) });
        break;
      case 'focus': {
        const f = ev.data as Focus;
        if (!(s.app?.config.settings.followAgent ?? true)) {
          const t = focusToTab(f);
          if (t) s.toast({ kind: 'agent', title: `Agent wants to show ${t.title}`, action: { label: 'Show', run: () => get().open(t) } });
          break;
        }
        const t = focusToTab(f);
        if (t) {
          s.open({ ...t, agent: true });
          if (f.message) s.toast({ kind: 'agent', title: f.message });
        }
        break;
      }
      case 'import': {
        if ((ev.data as { cleared?: boolean }).cleared) {
          set({ jobs: Object.fromEntries(Object.values(s.jobs).filter((j) => !finished(j)).map((j) => [j.id, j])) });
          break;
        }
        const j = ev.data as ImportJob;
        const was = s.jobs[j.id];
        const jobs = { ...s.jobs, [j.id]: j };
        set({ jobs });
        if (finished(j) && was && !finished(was)) {
          if (j.status === 'done') set({ libraryVersion: get().libraryVersion + 1 });
          // When a batch drains, summarize it.
          const batch = j.batch ? Object.values(jobs).filter((x) => x.batch === j.batch) : [];
          if (batch.length > 1 && batch.every(finished)) {
            const failed = batch.filter((x) => x.status === 'failed').length;
            const done = batch.filter((x) => x.status === 'done').length;
            s.toast({
              kind: failed ? 'warn' : 'ok',
              title: `Imported ${done} of ${batch.length} datasheets`,
              body: [failed && `${failed} failed`, batch.length - done - failed && `${batch.length - done - failed} already in the library`].filter(Boolean).join(', ') || undefined,
              action: { label: 'Show', run: () => get().setQueueOpen(true) },
            });
          }
        }
        break;
      }
      case 'library':
        set({ libraryVersion: s.libraryVersion + 1 });
        break;
      case 'project':
        set({ projectVersion: s.projectVersion + 1 });
        break;
      case 'drop': {
        const all = (ev.data as { paths?: string[] }).paths ?? [];
        const paths = all.filter((p) => /\.pdf$/i.test(p));
        // Dropped folders (no extension) are imported recursively.
        const folders = all.filter((p) => !/\.[a-z0-9]{1,5}$/i.test(p.split(/[\\/]/).pop() ?? ''));
        if (paths.length) s.importPaths(paths);
        for (const f of folders) s.queueImports({ folder: f, recursive: true });
        break;
      }
    }
  },
  refreshLibrary() {
    set({ libraryVersion: get().libraryVersion + 1 });
  },
}));

/** Reports whether an import job has ended. */
export function finished(j: ImportJob): boolean {
  return j.status === 'done' || j.status === 'existing' || j.status === 'failed';
}

/** Runs fn over items with at most n in flight. */
export async function pool<T>(items: T[], n: number, fn: (t: T) => Promise<void>): Promise<void> {
  const queue = [...items];
  const worker = async () => {
    for (let t = queue.shift(); t !== undefined; t = queue.shift()) await fn(t);
  };
  await Promise.all(Array.from({ length: Math.min(n, queue.length) }, worker));
}

/** Tells the user what an import produced and opens the first part. */
function announce(r: ImportResult) {
  const s = useStore.getState();
  const names = r.parts.map((p) => p.mpn);
  s.toast({
    kind: 'ok',
    title: r.existing ? `${r.datasheet.filename} is already in the library` : `Imported ${names.join(', ') || r.datasheet.filename}`,
    body: r.existing ? names.join(', ') : `${r.counts?.figures ?? 0} figures, ${r.counts?.parameters ?? 0} parameters, ${r.counts?.pins ?? 0} pins · ${r.took}`,
  });
  s.refreshLibrary();
  if (names[0]) s.open({ kind: 'part', title: names[0], params: { part: names[0], tab: 'overview' } });
}

/** The active tab, if any. */
export function useActiveTab(): Tab | undefined {
  return useStore((s) => s.tabs.find((t) => t.id === s.active));
}
