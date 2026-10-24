// What the app tells the backend about a block diagram it has drawn, for
// the agent: where each part went, and what the layout checks found.

import type { BlockDrawing, DrawnCell } from '../blockDiagram';
import { lint, type LayoutCheck, LAYOUT_CHECKS } from './lint';

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
}

/** Builds the report of a drawing. */
export function drawnReport(project: string, d: BlockDrawing, checks: readonly LayoutCheck[] = LAYOUT_CHECKS): DrawnReport {
  return { project, cells: d.cells, problems: lint(d, checks).map(({ check, group, text }) => ({ check, ...(group ? { group } : {}), text })) };
}
