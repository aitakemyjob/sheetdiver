import { describe, expect, it } from 'vitest';
import { dim, formatsLabel, settingsFormats, outlineRows, problemsOf, resetAction, stateInfo, textAttrs, toggleFormat, viewBox } from './cad';
import { recorded } from '../test/backend';
import type { CadLibrary, PartCad } from './types';

const sd = () => recorded('cad.part', { part: 'SD1234' }) as PartCad;
const lib = () => recorded('cad.library', null) as CadLibrary;

describe('the CAD library logic', () => {
  it('reads what the recorded part has: an edited symbol and an agent-drawn footprint', () => {
    const p = sd().packages[0];
    expect(p.package).toBe('SOT-23-6');
    expect(stateInfo(p.symbol!.state).label).toBe('Your edit');
    expect(stateInfo(p.footprint!.state).tone).toBe('accent');
    expect(resetAction(p.symbol)?.label).toBe('Discard my edit');
    expect(resetAction(p.footprint)?.label).toBe("Back to SheetDiver's");
    // The user renamed EN to ~{EN} in KiCad: the edit is what's shown.
    expect(p.symbol!.symbol!.pins.map((x) => x.name)).toContain('~{EN}');
    expect(p.footprint!.footprint!.from).toBe('datasheet land pattern');
  });

  it('shows the outline the footprint came from', () => {
    const rows = outlineRows(sd().packages[0].footprint!.package);
    expect(rows.find((r) => r[0] === 'Pitch')?.[2]).toBe('0.95');
    expect(rows.find((r) => r[0] === 'Span')?.[2]).toBe('2.6 – 3');
    expect(rows.find((r) => r[0] === 'Land pattern')?.[2]).toBe('pads 1.1 × 0.6, rows 2.6 apart (p. 31)');
    expect(dim({ min: 0.3, nom: 0.4, max: 0.5 })).toBe('0.3 / 0.4 / 0.5');
    expect(dim(undefined)).toBe('');
  });

  it('collects problems with what they are about', () => {
    const p = { ...sd().packages[0], match: [{ level: 'error' as const, text: 'symbol pins with no pad: 7' }] };
    expect(problemsOf(p)).toEqual([{ level: 'error', text: 'symbol pins with no pad: 7', about: 'Pins and pads' }]);
  });

  it('turns formats on and off, keeping at least the word none', () => {
    const l = lib();
    expect(formatsLabel(l)).toBe('KiCad');
    expect(toggleFormat(l, 'eagle', true)).toEqual(['eagle', 'kicad']);
    expect(toggleFormat(l, 'kicad', false)).toEqual(['none']);
    expect(formatsLabel({ ...l, formats: null })).toBe('no format (off)');
    expect(settingsFormats(undefined)).toEqual(['kicad']);
    expect(settingsFormats(['none'])).toEqual([]);
  });

  it('draws text turned when vertical, and frames the view', () => {
    expect(textAttrs({ kind: 'text', role: 'pinName', pts: [{ x: 1, y: 2 }], size: 1.27, vertical: true }).transform).toBe('rotate(-90 1 2)');
    expect(viewBox({ view: { x0: -2, y0: -1, x1: 2, y1: 1 }, items: [] })).toBe('-2 -1 4 2');
  });
});
