// Links in an agent's explanation open what they name:
//   #part/TPS63900                      the part
//   #page/TPS63900/3                    its datasheet's page 3
//   #figure/TPS63900/5                  its figure 5 (number or id)
//   #section/TPS63900/Electrical        the page its section starts on
//   #project/Sensor board               a project
import { api } from './api';
import { useStore } from './store';

/** A parsed agent link. */
export type AgentLink = { kind: 'part' | 'page' | 'figure' | 'section' | 'project'; target: string; arg: string };

/** Parses an agent link ("#page/TPS63900/3"); undefined for other links. */
export function parseAgentLink(href: string): AgentLink | undefined {
  const m = /^#(part|page|figure|section|project)\/([^/]+)(?:\/(.+))?$/.exec(href);
  if (!m) return undefined;
  const dec = (s: string) => {
    try {
      return decodeURIComponent(s);
    } catch {
      return s;
    }
  };
  return { kind: m[1] as AgentLink['kind'], target: dec(m[2]), arg: dec(m[3] ?? '') };
}

/** Opens what an agent link names; false when it can't be resolved. */
export async function openAgentLink(href: string): Promise<boolean> {
  const l = parseAgentLink(href);
  if (!l) return false;
  const s = useStore.getState();
  if (l.kind === 'project') {
    s.open({ kind: 'project', title: l.target, params: { project: l.target, nonce: Date.now() } });
    return true;
  }
  if (l.kind === 'part') {
    s.open({ kind: 'part', title: l.target, params: { part: l.target, nonce: Date.now() } });
    return true;
  }
  const d = await api.part(l.target).catch(() => undefined);
  const ds = d?.datasheet;
  if (!d || !ds) {
    s.toast({ kind: 'warn', title: `${l.target} isn't in the library` });
    return false;
  }
  const mpn = d.part.mpn;
  if (l.kind === 'page') {
    const page = Math.max(0, (Number(l.arg) || 1) - 1);
    s.open({ kind: 'page', title: `${mpn} p${page + 1}`, params: { ds: ds.id, page, part: mpn, nonce: Date.now() } });
    return true;
  }
  if (l.kind === 'figure') {
    const figs = await api.figures(ds.id).catch(() => []);
    const f = figs.find((x) => x.number === l.arg) ?? figs.find((x) => String(x.id) === l.arg);
    if (!f) {
      s.toast({ kind: 'warn', title: `${mpn} has no figure ${l.arg}` });
      return false;
    }
    s.open({ kind: 'figure', title: `Figure ${f.number || f.id}`, params: { id: f.id } });
    return true;
  }
  const secs = await api.sections(ds.id).catch(() => []);
  const want = l.arg.toLowerCase();
  const sec = secs.find((x) => x.title.toLowerCase() === want) ?? secs.find((x) => `${x.num} ${x.title}`.toLowerCase().includes(want));
  if (!sec) {
    s.toast({ kind: 'warn', title: `${mpn} has no section “${l.arg}”` });
    return false;
  }
  s.open({ kind: 'page', title: `${mpn} p${sec.page + 1}`, params: { ds: ds.id, page: sec.page, part: mpn, nonce: Date.now() } });
  return true;
}

/**
 * Prepares an explanation for rendering: Markdown links can't have spaces
 * in their target, but agents write section and project names with them
 * ("[Electrical](#section/TPS63900/Electrical Characteristics)").
 */
export function encodeAgentLinks(md: string): string {
  return md.replace(/\]\((#[^)\n]*)\)/g, (_, href: string) => `](${href.trim().replace(/ /g, '%20')})`);
}
