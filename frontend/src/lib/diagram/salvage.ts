// When a block diagram can't be laid out, the rest of it can still be:
// the diagram without the parts the layout trips on (and the passives
// drawn by them), so they don't hide the whole board, and the message can
// say which parts they were.

import type { Diagram } from '../types';

/** A part (or passive) left off a drawing because laying it out failed. */
export interface SkippedNode {
  /** Its diagram id ("part:3/Q4", "block:R1"). */
  node: string;
  /** Its title or label, as drawn elsewhere ("Q4 DMG2305UX"). */
  title: string;
  /** What laying it out threw. */
  error: string;
  /** The passives drawn by it, left off with it. */
  with: string[];
}

/** The most layouts tried, one part left off each, before giving up. */
export const MAX_TRIES = 40;

/** The text of something thrown. */
export function errorText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/**
 * The ids to try leaving off, parts first (a part's symbol is the likelier
 * culprit, and leaving one off takes its passives too), then passives; at
 * most MAX_TRIES.
 */
export function candidates(d: Diagram): string[] {
  return [...d.parts.map((p) => p.id), ...d.passives.map((q) => q.id)].slice(0, MAX_TRIES);
}

/**
 * A diagram without some nodes: them, the passives drawn by them (and
 * those chained after those), and every mention of them in nets, groups
 * and subgroups. Returns the diagram and, for each node left off, the
 * labels of its passives that went with it.
 */
export function without(d: Diagram, ids: string[]): { diagram: Diagram; passives: Map<string, string[]> } {
  // Which node each passive goes with: the one it hangs by, or the one
  // the passive it's chained after goes with.
  const owner = new Map<string, string>(ids.map((id) => [id, id]));
  for (let grew = true; grew; ) {
    grew = false;
    for (const q of d.passives) {
      const by = owner.get(q.part);
      if (!owner.has(q.id) && by) {
        owner.set(q.id, by);
        grew = true;
      }
    }
  }
  const keep = (x: string) => !owner.has(x);
  const passives = new Map<string, string[]>(ids.map((id) => [id, []]));
  for (const q of d.passives) {
    const by = owner.get(q.id);
    if (by && by !== q.id) passives.get(by)?.push(q.label);
  }
  return {
    diagram: {
      ...d,
      parts: d.parts.filter((p) => keep(p.id)),
      passives: d.passives.filter((q) => keep(q.id)),
      nets: d.nets.map((n) => ({ ...n, parts: n.parts.filter(keep) })),
      groups: d.groups.map((g) => ({ ...g, parts: g.parts.filter(keep) })),
      subgroups: d.subgroups.map((sg) => ({ ...sg, members: sg.members.filter(keep) })).filter((sg) => sg.members.length > 0),
    },
    passives,
  };
}

/** A diagram of one part alone, with the passives drawn by it. */
export function only(d: Diagram, id: string): Diagram {
  return without(
    d,
    d.parts.filter((p) => p.id !== id).map((p) => p.id),
  ).diagram;
}

/**
 * Lays out what it can of a diagram that failed with `error`: first
 * without every part that can't be laid out even alone (so a fault in how
 * one kind of part is drawn, met by several, is found), else without one
 * part or passive at a time. Returns the drawing and what was left off,
 * or throws a LayoutError.
 */
export async function salvage<T>(d: Diagram, error: string, layout: (d: Diagram) => Promise<T>): Promise<{ drawing: T; skipped: SkippedNode[] }> {
  const skip = (ids: string[], cut: Map<string, string[]>) => ids.map((node) => ({ node, title: titleOf(d, node), error, with: cut.get(node) ?? [] }));
  const tries = candidates(d);
  const alone: string[] = [];
  for (const id of tries.filter((x) => d.parts.some((p) => p.id === x))) {
    try {
      await layout(only(d, id));
    } catch {
      alone.push(id);
    }
  }
  if (alone.length && alone.length < d.parts.length) {
    const cut = without(d, alone);
    try {
      return { drawing: await layout(cut.diagram), skipped: skip(alone, cut.passives) };
    } catch {
      // Something else is wrong too: one at a time below.
    }
  }
  for (const id of tries) {
    const cut = without(d, [id]);
    try {
      return { drawing: await layout(cut.diagram), skipped: skip([id], cut.passives) };
    } catch {
      // Not this one.
    }
  }
  throw new LayoutError(error, tries.length, alone.map((id) => titleOf(d, id)));
}

/** A node's name for a message: a part's title, a passive's label, else its id. */
export function titleOf(d: Diagram, id: string): string {
  return d.parts.find((p) => p.id === id)?.title ?? d.passives.find((q) => q.id === id)?.label ?? id;
}

/** Says what was left off a drawing, for the user and the agent. */
export function skippedText(s: SkippedNode): string {
  const also = s.with.length ? ` (with ${s.with.join(', ')})` : '';
  return `${s.title}${also} couldn't be drawn and is left off: ${s.error}`;
}

/**
 * The error of a diagram that couldn't be laid out even with parts left
 * off: what it threw, and that leaving them off didn't help.
 */
export class LayoutError extends Error {
  constructor(
    readonly reason: string,
    readonly tried: number,
    /** The parts that fail even laid out alone, by title. */
    readonly alone: string[] = [],
  ) {
    super(
      alone.length
        ? `${reason} (${alone.join(', ')} ${alone.length > 1 ? 'each fail' : 'fails'} even drawn alone)`
        : tried
          ? `${reason} (it still failed with each of ${tried} parts and passives left off in turn)`
          : reason,
    );
    this.name = 'LayoutError';
  }
}
