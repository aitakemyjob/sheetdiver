// What the app tells the backend about a block diagram it has drawn, for
// the agent: where each part went, and what the layout checks found; or
// why it couldn't be drawn, whole or in part.

import type { BlockDrawing, DrawnCell } from '../blockDiagram';
import { lint, type LayoutCheck, LAYOUT_CHECKS } from './lint';
import { skippedText } from './salvage';

/** A layout problem as reported: the check, its group and what it found. */
export interface ReportedProblem {
  check: string;
  group?: string;
  text: string;
}

/** The report of a project's drawn block diagram (core.Drawn). */
export interface DrawnReport {
  project: string;
  cells: DrawnCell[];
  problems: ReportedProblem[];
  /** Why nothing could be drawn: set when the layout failed. */
  error?: string;
}

/** Builds the report of a drawing. */
export function drawnReport(project: string, d: BlockDrawing, checks: readonly LayoutCheck[] = LAYOUT_CHECKS): DrawnReport {
  // A part left off comes first: it's what's most wrong with the drawing.
  const skipped = (d.skipped ?? []).map((s) => ({ check: 'not drawn', text: skippedText(s) }));
  return { project, cells: d.cells, problems: [...skipped, ...lint(d, checks).map(({ check, group, text }) => ({ check, ...(group ? { group } : {}), text }))] };
}

/** The report of a diagram that couldn't be laid out at all. */
export function failedReport(project: string, error: string): DrawnReport {
  return { project, cells: [], problems: [], error };
}
