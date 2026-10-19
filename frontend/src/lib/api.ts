// Typed wrappers over the backend method table (internal/api).
import { getTransport } from './transport';
import type {
  AppState, Calculation, CalcField, CalcResponse, Datasheet, Figure, Hit, ImportResult, Note, Page, Parameter, PartDetail, Part,
  Pin, Project, ProjectDetail, ProjectPart, Rail, Recommendation, Register, Section, Settings, Table, Variant, Activity, Rect,
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
  deleteDatasheet: (id: number) => call<void>('datasheet.delete', { id }),
  reextract: (id: number) => call<ImportResult>('datasheet.reextract', { id }),
  page: (datasheetId: number, page: number) => call<Page>('page.get', { datasheetId, page }),

  parts: (f: { query?: string; category?: string; manufacturer?: string; limit?: number } = {}) => call<Part[]>('parts.list', f),
  categories: () => call<Record<string, number>>('parts.categories'),
  part: (part: string) => call<PartDetail>('part.detail', { part }),
  updatePart: (id: number, u: Partial<Pick<Part, 'mpn' | 'manufacturer' | 'description' | 'category' | 'package' | 'tags'>>) => call<Part>('part.update', { id, ...u }),
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
