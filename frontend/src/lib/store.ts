// Global UI state (zustand): app state, tabs, sidebar, toasts, agent
// activity and imports. Backend events arrive through the transport and
// are folded in by handleEvent.
import { create } from 'zustand';
import { api, uploadPDF } from './api';
import { getTransport } from './transport';
import { resetEngine, translateDatasheet } from './translate';
import { closeTab, focusToTab, openTab, type Tab } from './tabs';

/** A walkthrough: the agent's steps and the one being shown. */
export interface Walkthrough {
  title: string;
  steps: Focus[];
  index: number;
}
import { applyTheme, themeById, type Theme } from './themes';
import type { Activity, AppState, BusEvent, CatalogStatus, Config, Focus, ImportJob, ImportResult, MCPStatus, Part, Settings, TranslateStatus } from './types';

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
  /** The JLCPCB parts catalogue. */
  catalog?: CatalogStatus;
  translation?: TranslateStatus;
  /** A walkthrough the agent is taking the user through. */
  walkthrough?: Walkthrough;
  /** Shows a walkthrough step (opening what it shows). */
  walkTo: (index: number) => void;
  endWalkthrough: () => void;
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
  downloadCatalog: () => Promise<void>;
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
      const catalog = await api.catalogStatus().catch(() => undefined);
      const translation = await api.translateStatus().catch(() => undefined);
      set({ app, theme, ready: true, activity, jobs, catalog, translation });
      if (catalog?.stale) remindCatalog(catalog);
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
  walkTo(index) {
    const w = get().walkthrough;
    if (!w || index < 0 || index >= w.steps.length) return;
    set({ walkthrough: { ...w, index } });
    const t = focusToTab(w.steps[index]);
    if (t) get().open({ ...t, agent: true });
  },
  endWalkthrough() {
    set({ walkthrough: undefined });
  },
  async downloadCatalog() {
    try {
      set({ catalog: await api.catalogDownload() });
    } catch (e) {
      get().toast({ kind: 'err', title: 'Could not download the catalogue', body: (e as Error).message });
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
        if (f.kind === 'walkthrough') {
          const w: Walkthrough = { title: f.title ?? 'Walkthrough', steps: f.steps ?? [], index: 0 };
          if (!w.steps.length) break;
          if (!(s.app?.config.settings.followAgent ?? true)) {
            s.toast({ kind: 'agent', title: `Agent wants to walk you through: ${w.title}`, action: { label: 'Start', run: () => (set({ walkthrough: w }), get().walkTo(0)) } });
            break;
          }
          set({ walkthrough: w });
          get().walkTo(0);
          break;
        }
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
          if (j.status === 'done' && j.datasheetId && j.language && j.language !== 'en' && !j.translated) translateImport(j);
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
      case 'catalog': {
        const c = ev.data as CatalogStatus;
        const was = s.catalog;
        set({ catalog: c });
        if (was?.updating && !c.updating) {
          if (c.error) s.toast({ kind: 'err', title: 'Catalogue download failed', body: c.error });
          else s.toast({ kind: 'ok', title: 'Parts catalogue ready', body: `${c.parts.toLocaleString()} parts, ${c.inStock.toLocaleString()} in stock` });
        }
        break;
      }
      case 'translate': {
        const t = ev.data as TranslateStatus;
        const was = s.translation;
        set({ translation: t });
        if (was?.progress && !t.progress) {
          resetEngine();
          if (t.error) s.toast({ kind: 'err', title: 'Translation model download failed', body: t.error });
          else s.toast({ kind: 'ok', title: 'Offline translation ready', body: 'Chinese datasheets now have a Translate button.' });
        }
        break;
      }
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

/** Suggests refreshing an old parts catalogue (once per session). */
function remindCatalog(c: CatalogStatus) {
  const s = useStore.getState();
  s.toast({
    kind: 'info',
    title: `The parts catalogue is ${c.ageDays} days old`,
    body: 'JLCPCB stock and prices change daily.',
    action: { label: 'Refresh', run: () => useStore.getState().downloadCatalog() },
  });
}

/** Human-readable size in MB or GB. */
export function sizeText(bytes: number): string {
  return bytes >= 1 << 30 ? `${(bytes / (1 << 30)).toFixed(1)} GB` : `${Math.round(bytes / (1 << 20))} MB`;
}

/** Describes a catalogue download in progress. */
export function catalogProgressText(c?: CatalogStatus): string {
  const p = c?.progress;
  if (!p) return '';
  const pct = p.total ? ` ${Math.floor((100 * p.done) / p.total)}%` : '';
  switch (p.stage) {
    case 'downloading':
      return `Downloading${pct}${p.total ? ` of ${sizeText(p.total)}` : ''}`;
    case 'extracting':
      return `Unpacking${pct}`;
    case 'checking':
      return 'Checking';
    case 'done':
      return 'Done';
  }
  return 'Starting';
}

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

/** Imports waiting to be translated and re-extracted, one at a time. */
let translating: Promise<void> = Promise.resolve();

/**
 * Translates a datasheet that was imported in another language and
 * re-extracts it from the English, so its sections, tables, figures and
 * parameters are found as in English datasheets. Without the model, says
 * how to get it.
 */
function translateImport(j: ImportJob) {
  const s = useStore.getState();
  const model = s.translation?.models.find((m) => m.from === j.language && m.to === 'en');
  if (!model) return;
  if (!model.installed) {
    s.toast({
      kind: 'warn',
      title: `${j.name} is in ${model.label.split(' ')[0]}`,
      body: 'Download the translation model to extract it from an English translation (made on this computer).',
      action: { label: 'Settings', run: () => useStore.getState().open({ kind: 'settings', title: 'Settings', params: {} }) },
    });
    return;
  }
  const id = j.datasheetId!;
  translating = translating.then(async () => {
    try {
      await translateDatasheet(id, () => {});
      await api.reextract(id, true);
      useStore.getState().toast({ kind: 'ok', title: `Translated ${j.parts?.[0] ?? j.name}`, body: 'Extracted from its English translation' });
    } catch (e) {
      useStore.getState().toast({ kind: 'err', title: `Could not translate ${j.name}`, body: (e as Error).message });
    }
  });
}
