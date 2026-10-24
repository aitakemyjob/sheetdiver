import { describe, expect, it } from 'vitest';
import type { BlockDrawing } from '../blockDiagram';
import type { DiagramGroup } from '../types';
import { drawnReport } from './report';
import type { LayoutCheck } from './lint';

describe("the drawn diagram's report", () => {
  it('carries the cells and what the checks found, with their groups', () => {
    const d: BlockDrawing = {
      boxes: [], passives: [], wires: [], grounds: [], flags: [], dots: [], clusters: [], width: 100, height: 100,
      frames: [{ x: 0, y: 0, w: 50, h: 50, header: 40, group: { name: 'MCU' } as DiagramGroup }],
      cells: [{ node: 'U2', group: 'MCU', column: 0, row: 0 }],
    };
    const check: LayoutCheck = { name: 'test', find: () => [{ text: 'inside', at: { x: 10, y: 10 } }, { text: 'outside', at: { x: 90, y: 90 } }] };
    expect(drawnReport('board', d, [check])).toEqual({
      project: 'board',
      cells: [{ node: 'U2', group: 'MCU', column: 0, row: 0 }],
      problems: [{ check: 'test', group: 'MCU', text: 'inside' }, { check: 'test', text: 'outside' }],
    });
  });
});
