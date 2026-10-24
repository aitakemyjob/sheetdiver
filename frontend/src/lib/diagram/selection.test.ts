import { describe, expect, it } from 'vitest';
import type { BlockDrawing, Box, PlacedPassive, PlacedPin } from '../blockDiagram';
import type { DiagramPart, DiagramPassive } from '../types';
import { selectNet, toggleNet } from './selection';

const box = (id: string, nets: string[]): Box =>
  ({ x: 0, y: 0, w: 100, h: 100, body: { x: 0, y: 0, w: 100, h: 100 }, part: { id, title: id } as DiagramPart, pins: nets.map((net, i) => ({ name: `P${i}`, net, side: 'left', x: 0, y: 30 * i }) as PlacedPin) }) as Box;
const passive = (id: string, nets: string[]): PlacedPassive =>
  ({ x: 0, y: 0, w: 40, h: 40, passive: { id, label: id, nets } as DiagramPassive, ends: [{ x: 0, y: 0 }, { x: 0, y: 40 }], terminals: ['1', '2'], label: { x: 0, y: 0, anchor: 'start' } }) as PlacedPassive;

const d: BlockDrawing = {
  boxes: [box('U1', ['VIN', '3V3']), box('U2', ['3v3', 'SDA']), box('J1', ['VBUS'])],
  passives: [passive('C1', ['3V3', 'GND']), passive('R1', ['SDA', '3V3']), passive('C2', ['VIN', 'GND'])],
  wires: [{ net: '3V3', path: 'M100,30 H200' }, { net: 'SDA', path: 'M100,60 H200' }, { net: '3V3', path: 'M150,30 V50' }],
  dots: [{ x: 150, y: 30 }, { x: 150, y: 60 }],
  flags: [{ x: 10, y: 10, text: '3V3' }, { x: 20, y: 20, text: 'VIN' }],
  grounds: [], frames: [], clusters: [], cells: [], width: 300, height: 200,
};

describe('a picked net', () => {
  it('is its wires, junctions and flags, and every part and passive on it, whatever the case', () => {
    const s = selectNet(d, '3V3');
    expect([...s.wires]).toEqual([0, 2]);
    expect([...s.dots]).toEqual([0]); // the junction on SDA's wire isn't 3V3's
    expect([...s.flags]).toEqual([0]);
    expect([...s.parts]).toEqual(['U1', 'U2']);
    expect([...s.passives]).toEqual(['C1', 'R1']);
  });

  it('is picked by clicking its name, and let go by clicking it again', () => {
    expect(toggleNet(null, '3V3')).toBe('3V3');
    expect(toggleNet('3V3', '3v3')).toBeNull();
    expect(toggleNet('3V3', 'SDA')).toBe('SDA');
  });
});
