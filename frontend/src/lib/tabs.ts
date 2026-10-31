// Tabs are pure data; these helpers are side-effect free so they can be
// unit tested.
import type { Focus } from './types';

export type TabKind = 'part' | 'page' | 'figure' | 'project' | 'calc' | 'search' | 'settings' | 'category' | 'catalog' | 'compare' | 'cadEdit';

export interface Tab {
  id: string;
  kind: TabKind;
  title: string;
  params: Record<string, unknown>;
  /** Opened by the agent (shown with a dot until visited). */
  agent?: boolean;
}

/** Identity of a tab: opening the same thing twice reuses the tab. */
export function tabKey(kind: TabKind, params: Record<string, unknown>): string {
  switch (kind) {
    case 'part':
      return `part:${String(params.part ?? '').toUpperCase()}`;
    case 'page':
      return `page:${params.ds}`;
    case 'figure':
      return `figure:${params.id}`;
    case 'project':
      return `project:${String(params.project ?? '').toLowerCase()}`;
    case 'calc':
      return `calc:${params.kind ?? ''}:${params.part ?? ''}:${params.project ?? ''}`;
    case 'search':
      return 'search';
    case 'category':
      return `category:${params.category ?? ''}`;
    case 'settings':
      return 'settings';
    case 'catalog':
      return 'catalog';
    case 'cadEdit':
      return `cadEdit:${String(params.part ?? '').toUpperCase()}:${params.package ?? ''}:${params.what ?? ''}`;
    case 'compare':
      return `compare:${((params.parts as string[] | undefined) ?? []).join(',').toUpperCase()}`;
  }
}

/**
 * Opens a tab: an existing tab with the same key is updated (new params,
 * e.g. another page) and activated; otherwise the tab is appended.
 */
export function openTab(tabs: Tab[], t: Omit<Tab, 'id'>): { tabs: Tab[]; active: string } {
  const id = tabKey(t.kind, t.params);
  const i = tabs.findIndex((x) => x.id === id);
  if (i >= 0) {
    const next = tabs.slice();
    next[i] = { ...tabs[i], ...t, id, params: { ...tabs[i].params, ...t.params } };
    return { tabs: next, active: id };
  }
  return { tabs: [...tabs, { ...t, id }], active: id };
}

/** Closes a tab, activating its neighbour if it was active. */
export function closeTab(tabs: Tab[], id: string, active: string | undefined): { tabs: Tab[]; active: string | undefined } {
  const i = tabs.findIndex((t) => t.id === id);
  if (i < 0) return { tabs, active };
  const next = tabs.filter((t) => t.id !== id);
  if (active !== id) return { tabs: next, active };
  const n = next[Math.min(i, next.length - 1)];
  return { tabs: next, active: n?.id };
}

/**
 * Converts an agent focus request into a tab to open. Every tab carries
 * the agent's explanation and highlight (or clears an earlier one: an
 * existing tab's params are merged).
 */
export function focusToTab(f: Focus): Omit<Tab, 'id'> | undefined {
  const t = baseTab(f);
  return t && { ...t, params: { ...t.params, explain: f.explain, highlight: f.highlight } };
}

/** The tab for a focus request, without the agent's presentation. */
function baseTab(f: Focus): Omit<Tab, 'id'> | undefined {
  switch (f.kind) {
    case 'part':
      if (!f.part) return undefined;
      return { kind: 'part', title: f.part, params: { part: f.part, tab: f.tab ?? 'overview', query: f.query, nonce: Date.now() } };
    case 'page':
    case 'section':
      if (!f.datasheetId) return undefined;
      return { kind: 'page', title: `${f.part ?? 'Datasheet'} p${(f.page ?? 0) + 1}`, params: { ds: f.datasheetId, page: f.page ?? 0, box: f.box, part: f.part, message: f.message, nonce: Date.now() } };
    case 'figure':
      if (!f.figureId) return undefined;
      return { kind: 'figure', title: 'Figure', params: { id: f.figureId, message: f.message } };
    case 'project':
      if (!f.project) return undefined;
      return { kind: 'project', title: f.project, params: { project: f.project, tab: f.tab ?? 'overview', nonce: Date.now() } };
    case 'calc':
      if (!f.calc) return undefined;
      return { kind: 'calc', title: `${f.calc.kind} ${f.calc.part ?? ''}`.trim(), params: { kind: f.calc.kind, part: f.calc.part, project: f.calc.project, result: f.calc } };
    case 'search':
      return { kind: 'search', title: 'Search', params: { query: f.query ?? '' } };
    case 'category':
      if (!f.query) return undefined;
      return { kind: 'category', title: f.query, params: { category: f.query } };
    case 'compare':
      if (!f.parts?.length) return undefined;
      return { kind: 'compare', title: f.parts.join(' vs '), params: { parts: f.parts, keys: f.keys, notes: f.notes } };
    case 'catalog':
      return { kind: 'catalog', title: 'Parts catalogue', params: { query: f.query, category: f.category, subcategory: f.subcategory, filters: f.filters, lcsc: f.lcsc, nonce: Date.now() } };
  }
  return undefined;
}
