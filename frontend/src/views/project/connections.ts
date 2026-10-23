// What the project's connection tabs share.
import { api } from '../../lib/api';
import { useStore } from '../../lib/store';
import type { Assignment, ConnectionsChange, ProjectDetail } from '../../lib/types';

/** Applies a change to the connection table, telling the user what couldn't be done. */
export async function change(project: string, ch: ConnectionsChange, done: () => void) {
  const s = useStore.getState();
  try {
    const { problems } = await api.changeConnections(project, ch);
    if (problems.length) s.toast({ kind: 'warn', title: 'Not everything was applied', body: problems.join('\n') });
  } catch (e) {
    s.toast({ kind: 'err', title: 'Could not change the connections', body: (e as Error).message });
  }
  done();
}

/** Whether the agent's highlight names this node or link. */
export function marked(hl: string | undefined, ...names: (string | undefined)[]): boolean {
  return !!hl && names.some((n) => n?.toLowerCase() === hl);
}

/** What an entry can be named as: its designators, and each one when it has several ("R1,R2", "R1", "R2"). */
export function designators(d: string): string[] {
  const each = d.split(/[,;\s]+/).filter(Boolean);
  return each.length > 1 ? [d, ...each] : [d];
}

/** Every BOM designator (and each one of an entry with several) and block name. */
export function endpoints(d: ProjectDetail): string[] {
  return [...d.parts.flatMap((p) => designators(p.designator || p.mpn)), ...d.connections.blocks.map((b) => b.name)];
}

/** A part's or block's assignment, by the name it's shown with. */
export function assignmentOf(d: ProjectDetail, endpoint: string): Assignment | undefined {
  return d.connections.assignments.find((a) => a.endpoint.toLowerCase() === endpoint.toLowerCase());
}

/** The group a part or passive is drawn in when it's set in none: its supported part's; "" when none. */
export function inferredGroup(d: ProjectDetail, endpoint: string): string {
  const dg = d.connections.diagram;
  const named = (label: string) => label.split(',').some((l) => l.toLowerCase() === endpoint.toLowerCase());
  const q = dg.passives.find((p) => named(p.label));
  if (q) return q.group ?? '';
  return dg.parts.find((p) => named(p.title.split(' ')[0]) || p.title.toLowerCase() === endpoint.toLowerCase())?.group ?? '';
}

/**
 * The part a passive serves on the block diagram when nothing says so
 * (the only part its nets reach), by its title; "" when there's none.
 */
export function inferredSupport(d: ProjectDetail, endpoint: string): string {
  const dg = d.connections.diagram;
  const q = dg.passives.find((p) => p.label.split(',').some((l) => l.toLowerCase() === endpoint.toLowerCase()));
  return q?.supports ? (dg.parts.find((p) => p.id === q.supports)?.title.split(' ')[0] ?? '') : '';
}
