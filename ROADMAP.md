# Roadmap

Where SheetDiver is heading. Plans change; if something here matters to you,
say so in [Discussions](https://github.com/aitakemyjob/sheetdiver/discussions).

## 0.2 — Better extraction

- **Graph digitizing:** read curves off vector graphs (most TI and ADI
  graphs are paths) into data points, so "efficiency at 2 A" is a lookup.
- **Register maps:** structured extraction of register tables (address,
  fields, reset) instead of leaving them to the agent.
- **Multi-package pin tables** (QFN vs BGA columns) and ST-style tables
  with merged cells.
- A larger corpus: ADI/Maxim, onsemi, Nexperia, Vishay, Renesas, Nordic
  (sites that need a browser to download).

## 0.3 — Deeper design help

- **More calculators:** buck-boost and SEPIC, charger termination and
  thermal, LED drivers, op-amp gain/bandwidth, RC filters, I²C pull-ups,
  crystal load capacitors.
- **Power tree view** of a project: rails, regulators and loads as a
  graph, with efficiency through the tree.
- **Component search** beyond the library's key parameters: packages,
  features and conditions ("buck, VIN ≥ 24 V, IOUT ≥ 3 A, SOT-23").
- **BOM export** and checking a schematic netlist's parts against the
  library (CSV/KiCad BOM import is done).

## 0.4 — Sharing

- Library sync through a git repository or shared folder, with merge of
  recorded data.
- Distributor APIs (Digi-Key, Mouser, with your own keys) for parts the
  JLCPCB catalogue doesn't carry, and lifecycle status from them.
- Parametric filters in the parts catalogue (≥ / ≤ on its attributes).
