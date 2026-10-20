// Typed wrappers over the backend method table (internal/api).
import { getTransport } from './transport';
import type {
  AppState, Calculation, CalcField, CalcResponse, Datasheet, Figure, Hit, ImportResult, Note, Page, Parameter, PartDetail, Part,
  Pin, Project, ProjectDetail, ProjectPart, Rail, Recommendation, Register, Section, Settings, Table, Variant, Activity, Rect,
  CategoryInfo, CompareFilter, CompareResult, BOMResult, ImportJob, AltResult, UpdateCheck, CatalogItem, CatalogStatus, CatalogCategory, CatalogParam, CatalogFilter, DeletePlan, DeleteResult,
  TranslateStatus,
} from './types';

/** Calls a backend method. */
async function call<T>(method: string, args?: unknown): Promise<T> {
  return (await getTransport()).invoke<T>(method, args);
}

export const api = {
  state: () => call<AppState>('app.state'),
  saveSettings: (s: Settings) => call<void>('settings.update', s),
  activity: () => call<Activity[]>('activity.list'),
  uiContext: (c: { view: string; part?: string; datasheetId?: number; page?: number; figureId?: number; project?: string; selection?: string }) => call<void>('ui.context', c),
  pickPDFs: () => call<{ paths: string[] | null }>('files.pickPDFs'),

  importPath: (req: { path?: string; url?: string; mpns?: string[]; manufacturer?: string; category?: string; force?: boolean }) => call<ImportResult>('datasheets.import', req),
  datasheets: () => call<Datasheet[]>('datasheets.list'),
  datasheet: (id: number) => call<{ datasheet: Datasheet; pages: Page[]; parts: Part[] }>('datasheet.get', { id }),
  updateDatasheet: (d: Datasheet) => call<void>('datasheet.update', d),
  openDatasheet: (id: number) => call<{ path: string }>('datasheet.open', { id }),
  deleteDatasheet: (id: number) => call<void>('datasheet.delete', { id }),
  reextract: (id: number, translated = false) => call<ImportResult>('datasheet.reextract', { id, translated }),
  translationSources: (id: number) => call<{ lang: string; texts: string[] }>('datasheet.translationSources', { id }),
  page: (datasheetId: number, page: number) => call<Page>('page.get', { datasheetId, page }),

  parts: (f: { query?: string; category?: string; manufacturer?: string; favorite?: boolean; status?: string; limit?: number } = {}) => call<Part[]>('parts.list', f),
  categories: () => call<Record<string, number>>('parts.categories'),
  categoryList: () => call<CategoryInfo[]>('categories.list'),
  compare: (req: { category: string; keys?: string[]; filters?: CompareFilter[]; query?: string; sort?: string; desc?: boolean; favorite?: boolean; status?: string }) => call<CompareResult>('compare', req),
  part: (part: string) => call<PartDetail>('part.detail', { part }),
  updatePart: (id: number, u: Partial<Pick<Part, 'mpn' | 'manufacturer' | 'description' | 'category' | 'package' | 'tags' | 'favorite' | 'status'>>) => call<Part>('part.update', { id, ...u }),
  startImports: (req: { paths?: string[]; urls?: string[]; mpns?: string[]; folder?: string; recursive?: boolean }) => call<ImportJob[]>('imports.start', req),
  importJobs: () => call<ImportJob[]>('imports.list'),
  retryImport: (id: number) => call<ImportJob>('imports.retry', { id }),
  clearImports: () => call<null>('imports.clear'),
  importBOM: (req: { text?: string; path?: string; filename?: string; project?: string; fetch: boolean }) => call<BOMResult>('bom.import', req),
  pickFolder: () => call<{ path: string }>('files.pickFolder'),
  alternatives: (req: { part: string; keys?: string[]; discouraged?: boolean; limit?: number }) => call<AltResult>('part.alternatives', req),
  projectReport: (project: string) => call<{ markdown: string }>('project.report', { project }),
  checkUpdate: (id: number) => call<UpdateCheck>('datasheet.checkUpdate', { id }),
  applyUpdate: (id: number) => call<ImportResult>('datasheet.applyUpdate', { id }),
  checkAllUpdates: () => call<UpdateCheck[]>('datasheets.checkUpdates'),
  saveFile: (name: string, data: string) => call<{ path: string }>('file.save', { name, data }),
  deletePreview: (part: string) => call<DeletePlan>('delete.preview', { part }),
  deleteApply: (part: string, keepInProjects: boolean) => call<DeleteResult>('delete.apply', { part, keepInProjects }),
  translateStatus: () => call<TranslateStatus>('translate.status'),
  translateDownload: (from: string, to: string) => call<TranslateStatus>('translate.download', { from, to }),
  translateRemove: (from: string, to: string) => call<TranslateStatus>('translate.remove', { from, to }),
  translations: (lang: string, texts: string[]) => call<Record<string, string>>('translations.get', { lang, texts }),
  saveTranslations: (items: { lang: string; source: string; text: string }[]) => call<null>('translations.save', { items }),
  catalogStatus: () => call<CatalogStatus>('catalog.status'),
  catalogDownload: () => call<CatalogStatus>('catalog.download'),
  catalogRemove: () => call<CatalogStatus>('catalog.remove'),
  catalogSearch: (q: { text: string; inStock?: boolean; basic?: boolean; subcategory?: string; attrs?: CatalogFilter[]; sort?: string; desc?: boolean; limit?: number; offset?: number }) =>
    call<CatalogItem[]>('catalog.search', q),
  catalogParams: (q: { text: string; subcategory?: string; inStock?: boolean; basic?: boolean; attrs?: CatalogFilter[] }) => call<CatalogParam[]>('catalog.params', q),
  catalogCategories: () => call<CatalogCategory[]>('catalog.categories'),
  catalogSubcategories: () => call<string[]>('catalog.subcategories'),
  catalogImport: (lcsc: string[]) => call<ImportJob[]>('catalog.import', { lcsc }),
  addPart: (p: Partial<Part>) => call<Part>('part.add', p),
  deletePart: (id: number) => call<void>('part.delete', { id }),
  setVariants: (id: number, variants: Partial<Variant>[]) => call<void>('part.variants', { id, variants }),

  parameters: (datasheetId: number, f: { key?: string; section?: string; query?: string; mpn?: string } = {}) => call<Parameter[]>('parameters.list', { datasheetId, ...f }),
  recordParameters: (datasheetId: number, parameters: Partial<Parameter>[]) => call<Parameter[]>('parameters.record', { datasheetId, parameters }),
  deleteParameters: (datasheetId: number, ids: number[]) => call<void>('parameters.delete', { datasheetId, ids }),
  sections: (datasheetId: number) => call<Section[]>('sections.list', { id: datasheetId }),
  section: (id: number) => call<Section>('section.get', { id }),
  figures: (datasheetId: number, f: { kind?: string; query?: string; mpn?: string } = {}) => call<Figure[]>('figures.list', { datasheetId, ...f }),
  figure: (id: number) => call<{ figure: Figure; notes: Note[]; text: string }>('figure.get', { id }),
  updateFigure: (id: number, u: Partial<Omit<Figure, 'id'>>) => call<Figure>('figure.update', { id, ...u }),
  addFigure: (f: Partial<Figure>) => call<Figure>('figure.add', f),
  tables: (datasheetId: number) => call<Table[]>('tables.list', { id: datasheetId }),
  recordTable: (id: number, u: { title?: string; description?: string; header?: string[]; rows?: string[][] }) => call<Table>('table.record', { id, ...u }),
  pins: (datasheetId: number) => call<Pin[]>('pins.list', { id: datasheetId }),
  registers: (datasheetId: number) => call<Register[]>('registers.list', { id: datasheetId }),
  recommendations: (datasheetId: number, mpn?: string) => call<Recommendation[]>('recommendations.list', { datasheetId, mpn }),
  deleteRecommendation: (id: number) => call<void>('recommendations.delete', { id }),
  search: (query: string, o: { kinds?: string[]; datasheetId?: number; limit?: number } = {}) => call<Hit[]>('search', { query, ...o }),
  notes: (kind: string, id: string) => call<Note[]>('notes.list', { kind, id }),
  addNote: (targetKind: string, targetId: string, body: string, datasheetId?: number) => call<Note>('notes.add', { targetKind, targetId, body, datasheetId }),
  updateNote: (id: number, body: string) => call<Note>('notes.update', { id, body }),
  deleteNote: (id: number) => call<void>('notes.delete', { id }),

  projects: () => call<Project[]>('projects.list'),
  createProject: (name: string, description = '') => call<Project>('projects.create', { name, description }),
  updateProject: (p: Project) => call<void>('projects.update', p),
  deleteProject: (id: number) => call<void>('projects.delete', { id }),
  setActiveProject: (name: string) => call<void>('projects.setActive', { name }),
  project: (project: string) => call<ProjectDetail>('project.detail', { project }),
  setRail: (r: Partial<Rail> & { projectId: number; name: string }) => call<Rail>('rails.set', r),
  deleteRail: (projectId: number, name: string) => call<void>('rails.delete', { projectId, name }),
  addProjectPart: (project: string, part: string, pp: Partial<ProjectPart>) => call<ProjectPart>('projectParts.add', { project, part, ...pp }),
  updateProjectPart: (pp: ProjectPart) => call<void>('projectParts.update', pp),
  removeProjectPart: (projectId: number, id: number) => call<void>('projectParts.remove', { projectId, id }),

  calcFields: (kind: string) => call<CalcField[]>('calc.fields', { kind }),
  calc: (req: { kind: string; part?: string; project?: string; inputs?: Record<string, unknown>; save?: boolean; title?: string }) => call<CalcResponse>('calc.run', req),
  calculations: (projectId = 0) => call<Calculation[]>('calc.list', { projectId }),
  deleteCalculation: (id: number) => call<void>('calc.delete', { id }),
};

/** URL of a rendered page (or region) PNG. */
export function pageImageURL(datasheetId: number, page: number, dpi = 110, box?: Rect): string {
  let u = `/api/page.png?sd=1&ds=${datasheetId}&page=${page}&dpi=${dpi}`;
  if (box) u += `&x0=${box.x0.toFixed(1)}&y0=${box.y0.toFixed(1)}&x1=${box.x1.toFixed(1)}&y1=${box.y1.toFixed(1)}`;
  return u;
}

/** URL of a rendered figure PNG. */
export function figureImageURL(id: number, dpi = 150, version = ''): string {
  return `/api/figure.png?sd=1&id=${id}&dpi=${dpi}${version ? `&v=${encodeURIComponent(version)}` : ''}`;
}

/** URL of a rendered table PNG. */
export function tableImageURL(id: number, dpi = 110, version = ''): string {
  return `/api/table.png?sd=1&id=${id}&dpi=${dpi}${version ? `&v=${encodeURIComponent(version)}` : ''}`;
}

/** URL of a datasheet's original PDF. */
export function datasheetURL(id: number): string {
  return `/api/datasheet.pdf?sd=1&ds=${id}`;
}

/**
 * Opens a datasheet's original PDF: in the system PDF viewer in the
 * desktop app (its webview can't open windows), in a new tab in the
 * browser.
 */
export async function openPDF(id: number, desktop: boolean): Promise<void> {
  if (desktop) {
    await api.openDatasheet(id);
    return;
  }
  window.open(datasheetURL(id), '_blank', 'noopener');
}

/** Uploads a PDF file into the library. */
export async function uploadPDF(file: File, opts: { mpns?: string; manufacturer?: string; category?: string } = {}): Promise<ImportResult> {
  const q = new URLSearchParams({ filename: file.name });
  if (opts.mpns) q.set('mpns', opts.mpns);
  if (opts.manufacturer) q.set('manufacturer', opts.manufacturer);
  if (opts.category) q.set('category', opts.category);
  const res = await fetch(`/api/upload?${q}`, { method: 'POST', body: file, headers: { 'X-SheetDiver': '1', 'Content-Type': 'application/pdf' } });
  const body = (await res.json()) as { result?: ImportResult; error?: string };
  if (body.error) throw new Error(body.error);
  return body.result!;
}

/**
 * Saves text as a file: through the desktop app's save dialog when it has
 * one (returns the path, or '' if cancelled), else as a browser download
 * (returns the file name).
 */
export async function saveText(name: string, text: string, mime: string, desktop: boolean): Promise<string> {
  if (desktop) {
    const bytes = new TextEncoder().encode(text);
    let bin = '';
    for (const b of bytes) bin += String.fromCharCode(b);
    return (await api.saveFile(name, btoa(bin))).path;
  }
  const a = document.createElement('a');
  a.href = URL.createObjectURL(new Blob([text], { type: mime }));
  a.download = name;
  a.click();
  URL.revokeObjectURL(a.href);
  return name;
}

/** A catalogue part's page at LCSC (buy) or JLCPCB (assembly), from its LCSC code. */
export function shopURL(lcsc: string, shop: 'lcsc' | 'jlcpcb'): string {
  const code = lcsc.trim().toUpperCase();
  if (!/^C\d{1,10}$/.test(code)) throw new Error(`${lcsc} is not an LCSC part code`);
  return shop === 'jlcpcb' ? `https://jlcpcb.com/partdetail/${code}` : `https://www.lcsc.com/product-detail/${code}.html`;
}

/**
 * Opens a catalogue part at LCSC or JLCPCB: in the system browser from the
 * desktop app (the webview can't open tabs), else in a new tab.
 */
export async function openShop(lcsc: string, shop: 'lcsc' | 'jlcpcb', desktop: boolean): Promise<void> {
  const url = shopURL(lcsc, shop);
  if (desktop) {
    await getTransport().then((t) => t.invoke('catalog.openShop', { lcsc, shop }));
    return;
  }
  window.open(url, '_blank', 'noopener');
}
