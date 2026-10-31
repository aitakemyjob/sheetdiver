import type { PackageDim, PartPackage } from './types';

/** What a package dimension's key measures, in words. */
const keyLabels: Record<string, string> = {
  height: 'Height',
  standoff: 'Standoff',
  bodyThickness: 'Body thickness',
  leadWidth: 'Lead width',
  leadThickness: 'Lead thickness',
  bodyLength: 'Body length',
  bodyWidth: 'Body width',
  span: 'Lead span',
  pitch: 'Pitch',
  leadLength: 'Lead length',
  padLength: 'Pad length',
  padWidth: 'Pad width',
  pins: 'Pins',
  rowSpan: 'Row span',
  gap: 'Gap',
  outerSpan: 'Outer span',
  outerWidth: 'Outer width',
};

/** A dimension's label: what it measures, else its description. */
export function dimLabel(d: PackageDim): string {
  return (d.key && keyLabels[d.key]) || d.description || '';
}

/** A limit's cell: the value in mm, or a dash. */
export function limitCell(v: number | undefined): string {
  return v === undefined || v === null ? '–' : String(+v.toFixed(4));
}

/** Where a dimension came from, as a badge, or null for the extractor's own. */
export function sourceBadge(source: string): { label: string; tone: 'accent' | 'violet' } | null {
  if (source === 'agent') return { label: 'Agent', tone: 'accent' };
  if (source === 'user') return { label: 'You', tone: 'violet' };
  return null;
}

/** A package's heading: name, drawing code and JEDEC outline. */
export function packageTitle(p: PartPackage): string {
  return [p.name, p.code && `(${p.code})`, p.reference && `JEDEC ${p.reference}`].filter(Boolean).join(' ');
}

/** The rows of a package an agent or the user recorded, which may be removed. */
export function recordedRows(p: PartPackage) {
  return p.rows.filter((r) => r.source !== 'auto');
}
