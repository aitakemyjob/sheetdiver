# Roadmap

Where SheetDiver is heading. Plans change with what people need: if something
here matters to you, or is missing, say so in
[Discussions](https://github.com/aitakemyjob/sheetdiver/discussions) or 👍 the
issue. Each release keeps the library format compatible and re-extracts
existing datasheets automatically when extraction improves.

**Shipped: 0.1** — import and extraction, the library and page viewer,
projects with rail checks, calculators, comparisons and alternatives,
datasheet revisions, the JLCPCB parts catalogue, BOM import and design-review
reports, and the MCP server. See the [changelog](CHANGELOG.md).

## 0.2 — Numbers you can trust

Extraction gives a good draft; this release makes it measurably better and
makes reviewing it fast.

- **Review mode:** step through a datasheet's key parameters next to the
  page region each came from and confirm, correct or reject them with one
  key. Confirmed values are marked and preferred everywhere.
- **Extraction coverage report** per datasheet and across the library
  (which keys were found, which are missing, which are unconfirmed), so you
  and your agent know what to check.
- **Graph digitizing:** read curves from vector graphs (most TI and ADI
  graphs are paths) into data points, so "efficiency at 2 A, 12 V in" is a
  lookup, not a squint.
- **Register maps:** structured extraction of register tables (address,
  fields, bits, reset values).
- **Harder tables:** dense multi-column MCU tables, ST-style merged cells,
  footnoted conditions.
- **A bigger corpus:** Microchip, Renesas, Nordic, onsemi, Vishay,
  Infineon and ADI datasheets, with expected values, so every fix is
  measured and nothing regresses. Contributions welcome: the
  [Datasheet extracts badly](https://github.com/aitakemyjob/sheetdiver/issues/new/choose)
  issue template is the fastest way to help.

## 0.3 — Sourcing

From "which part?" to "can I buy it and build it?".

- **Distributor sources:** Digi-Key and Mouser (with your own API keys,
  stored locally) next to the JLCPCB catalogue: stock, price breaks,
  lifecycle and datasheet links for parts JLCPCB doesn't carry. Lifecycle
  feeds part status (NRND, EOL).
- **Project sourcing view:** per BOM line, availability and price at each
  source, JLC basic/extended (setup fees), low-stock warnings, and cheaper
  in-stock alternatives from the catalogue.
- **BOM export:** costed CSV, and JLCPCB assembly BOMs with LCSC codes.
- **KiCad:** build a project (parts, designators, rails) from a KiCad
  schematic or netlist, and keep it in sync. (The other way, a project
  exported as a KiCad schematic with a layout guide, is in as a preview.)

## 0.4 — Deeper design help

- **Power tree view** of a project: rails, regulators and loads as a graph,
  with efficiency and dissipation through the tree (from the efficiency
  curves once they're digitized).
- **More calculators:** buck-boost and SEPIC, charger termination and
  thermal, LED drivers, op-amp gain and bandwidth, RC and LC filters, I²C
  pull-ups, crystal load capacitors, thermal budgets.
- **Pin-level checks:** I/O voltages against each pin's absolute maximum,
  logic thresholds between connected parts.
- **Reference designs:** turn a datasheet's typical application and
  recommended components into a starting BOM.

## 0.5 — Simulation

Your agent simulates the board, and the results stay with the project.
([#87](https://github.com/aitakemyjob/sheetdiver/issues/87))

- **Agent-run simulations:** the agent decides what's worth checking
  (inrush on hot-plug, a power-mux switchover, start-up, ripple) and runs
  as many configurations as it needs with a bundled ngspice: sanitized
  netlists, each run in its own directory with a time limit.
- **Saved per project:** every run keeps its netlist, models, raw data
  and measurements, and is marked stale when the board changes, so a
  result can always be reproduced or rerun.
- **Presentations and graphs:** the agent writes up each result set
  (plots, measurements against targets, conclusions); the app shows it,
  with interactive graphs over the raw data for zooming, cursors and
  comparing runs.
- **From the board:** netlists built from the connection table, SPICE
  models imported alongside datasheets, and every stand-in for a part
  without a model called out.

## Toward 1.0

- **Shared libraries:** sync through a git repository or shared folder,
  merging what each person recorded.
- **Signed and notarized builds** for macOS and Windows, and packages in
  Homebrew, winget and Flathub.
- **Extensible extraction:** rules for a manufacturer's table style as data
  rather than code, so contributors can add them without touching the
  extractor.
- Accessibility and localization of the UI.

## Not planned

- **A schematic or PCB editor.** SheetDiver works next to KiCad, Altium and
  friends, not instead of them.
- **A cloud service or account.** The library, the catalogue and the MCP
  server stay on your machine; SheetDiver only goes online when you import
  or download something.
- **Bundling datasheets or catalogue data.** Datasheets belong to their
  manufacturers and the catalogue to JLCPCB; SheetDiver downloads them for
  you on request.
