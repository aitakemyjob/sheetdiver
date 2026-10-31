// How the library browser arranges parts: groups of categories, as a CAD
// tool's component panel arranges them, each category under one group
// and anything unknown under Other.

/** A group of categories in the browser's tree. */
export interface Group {
  id: string;
  label: string;
  categories: string[];
}

/** The groups, in the order shown. */
export const groups: Group[] = [
  { id: 'power', label: 'Power', categories: ['buck', 'boost', 'buck-boost', 'ldo', 'charger', 'fuel-gauge', 'battery-protection', 'load-switch', 'efuse', 'current-monitor', 'gate-driver', 'power'] },
  { id: 'processors', label: 'Processors', categories: ['mcu'] },
  { id: 'analog', label: 'Analog', categories: ['amplifier', 'adc', 'dac', 'reference', 'sensor'] },
  { id: 'interface', label: 'Interface', categories: ['interface'] },
  { id: 'discretes', label: 'Discrete semiconductors', categories: ['mosfet-n', 'mosfet-p', 'mosfet', 'npn', 'pnp', 'diode', 'schottky', 'zener', 'led', 'tvs', 'esd'] },
  { id: 'passives', label: 'Passives', categories: ['resistor', 'capacitor', 'capacitor-polarised', 'inductor', 'ferrite', 'fuse', 'crystal', 'battery'] },
  { id: 'electromechanical', label: 'Connectors & modules', categories: ['connector', 'switch', 'module'] },
  { id: 'other', label: 'Other', categories: [] },
];

const groupByCategory = new Map<string, Group>();
for (const g of groups) for (const c of g.categories) groupByCategory.set(c, g);

/** The group a category belongs to: Other for one no group names. */
export function groupOf(category: string): Group {
  return groupByCategory.get(category) ?? groups[groups.length - 1];
}
