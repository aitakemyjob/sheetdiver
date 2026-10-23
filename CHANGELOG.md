# Changelog

Notable changes to SheetDiver. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org). Each
[GitHub release](https://github.com/aitakemyjob/sheetdiver/releases) also lists
every merged pull request.

## [Unreleased]

### Changed
- **Windows: `sheetdiver-mcp.exe` works like `sheetdiver mcp`.** It relays
  to the open app, or serves your library itself while the app is closed,
  instead of starting SheetDiver. Claude Desktop now starts a short-lived
  copy of each server to check its protocol version, and a copy that
  opened the app (or kept Claude waiting while it did) broke that check.
  The Windows extension's *Start SheetDiver* setting is gone.
- **One SheetDiver window at a time.** Launching SheetDiver again brings
  the open window forward instead of opening a second app that competes
  for the MCP port and the library. (Apps on different settings files,
  `SHEETDIVER_CONFIG`, still run side by side.)

### Fixed
- **macOS/Linux: the Claude Desktop extension exited at once ("version
  negotiation failed", "server transport closed unexpectedly").** Its
  launcher handed SheetDiver its own stdio, which the Node that Claude
  Desktop runs extensions with may not pass on, so SheetDiver found its
  input closed and stopped without a word. The launcher now pipes the
  messages through itself, and reports on stderr (in Claude Desktop's
  server log) why SheetDiver stopped if it does. Save the extension again
  from the connect dialog and reinstall it.
- **Claude Desktop couldn't start SheetDiver ("version negotiation
  failed").** Claude Desktop probes a server by sending a request and
  closing its input straight away, and `sheetdiver mcp` stopped without
  answering. It now answers every request it has read before it stops,
  both when it relays to the app and when it serves the library itself.

## [0.1.0] - 2026-10-21

The first release: a datasheet library for electronics design, shared with
your AI agent over MCP.

### Datasheets in, structured data out
- **Import** PDFs by drag-and-drop (files or folders), file picker, URL
  (product pages that link to the PDF too), part number (looked up at TI,
  ST, ADI/Maxim, onsemi, Nexperia and Diodes, then the parts catalogue) or
  `sheetdiver import`. Several import in parallel, followed in an import
  queue with progress and retry. PDFs are read by PDFium compiled to
  WebAssembly: no cgo, no external tools, no filesystem access.
- **Part numbers that resolve:** base parts, family patterns (`TPS56x200`,
  `STM32U545xx`), orderable codes (`TPS563200DDCR`, `TLV75733PDBVR`,
  `MCP1700T-3302E/TT`, `MAX77801EWP+T`) and unlisted ordering codes by
  prefix (`STM32U545VET6Q` → `STM32U545VE`).
- **Extraction:** the section tree (outline or headings, two-column reading
  order); every captioned figure cut out and classified (graph, schematic,
  waveform, block diagram, pinout, timing, layout, package) with axes from
  "Y vs X" captions; spec tables as min/typ/max parameters with conditions,
  section and a canonical key (`vin`, `iout`, `fsw`, `vref`, `ilim`, `iq`,
  `rdson`, `qg`…); pin tables, per package; every table as an image, with
  irregular ones flagged for the agent to transcribe; recommended
  inductors and capacitors; the datasheet revision (TI, ST, ADI/Maxim,
  Microchip, Diodes, AOS styles).
- Extraction knows the usual traps: names centred across condition rows,
  typ printed half a line off, ±% tolerances, current limits in normal
  operation vs start-up vs input, per-pin absolute-maximum rows, thresholds
  that aren't operating ranges, µ and Ω set in Symbol fonts, and output
  ratings stated only on the first page. A corpus of 19 real datasheets
  guards it (`make corpus && make test-corpus`); `sheetdiver inspect`
  shows what it finds.
- **Chinese and small-vendor datasheets** (TP4056, DW01A and the like):
  Chinese headings, spec and pin tables, uncaptioned figures and pins
  described in prose are extracted too.
- **Offline translation:** a part's text (sections, tables, parameters,
  figures) can be translated from Chinese to English inside the app, with
  an electronics glossary; non-English imports are translated
  automatically once it's set up. Translation runs locally (Mozilla's
  Bergamot engine); the model (about 55 MB, checked by SHA-256) is
  downloaded only when you ask, from Settings › Translation. Nothing is
  sent anywhere to translate.
- Section text reads as written: paragraphs, headings, lists, tables and
  figure references, instead of raw lines.
- **Datasheet revisions:** check one datasheet or the whole library for a
  newer revision, see which spec rows changed, and update in place. The
  same datasheet from another URL is recognised instead of duplicated.
  When the extractor improves, existing datasheets are re-extracted in the
  background; what you and your agent recorded is always kept.

### Library
- SQLite with full-text search over parts, pages, sections, figures,
  parameters, pins, registers, tables, recommendations and notes. Every
  value records whether it came from the extractor, the agent or you;
  recorded values win and survive re-extraction.
- Part view: key parameters, editable parameter table, figure gallery and
  editor, pins, sections, registers, tables, notes, variants and
  alternatives. Page viewer with figure and table overlays, highlighted
  parameter regions and drag-to-select. The PDF opens in your viewer.
- **Favorites and status** (preferred, approved, avoid, NRND, EOL), used by
  filters, comparisons, alternatives and project checks.
- **Compare by type:** a component type's parts side by side on the
  parameters that matter for it, sorted and filtered with units
  ("VIN ≥ 24 V, IOUT ≥ 3 A"), every value linked to its page; CSV export.
- **Alternatives:** library parts that could replace a part, compared key
  by key in the direction that matters; drop-ins first.
- **Delete from the library:** a confirmation lists what goes: the
  datasheet with all its parts, what you and agents recorded for it,
  notes, and the projects that use the parts (kept there without a
  datasheet, or removed with them, your choice). Agents can't delete.

### Parts catalogue (JLCPCB / LCSC)
- One click downloads the open [jlcparts](https://github.com/yaqwsx/jlcparts)
  dataset (about 2.5 million parts, republished from JLCPCB three times a
  day; about 200 MB), searched offline with a reminder to refresh it.
- Browse by category, search by part number or specs, and filter on each
  category's own parameters (Drain to Source Voltage ≥ 30V, RDS(on) ≤ 10mΩ,
  Type = N-Channel; supply ranges compare as "accepts"), with stock, price,
  JLC basic/extended library and the parameters as sortable columns.
- **Package filter** (several at once: SOT-23, SOT-23-3…), text filters
  with several values, and = as well as ≥ and ≤ on numbers (Resistance =
  10k matches 10.0kΩ, not 10.2kΩ; ranges match when they contain the
  value), with the category's common values suggested. Each filter's
  values follow the other filters, with counts, so after "VDS ≥ 30 V" the
  package list shows only packages that still have matching parts.
- Open a part at LCSC or JLCPCB from its details or its LCSC code.
- Tick parts to import their datasheets. Part-number lookups and BOM import
  fall back to its datasheet links.

### Projects and design
- **Projects** for a board: BOM with designators, rails and currents;
  per-rail load including regulator input currents derived from their
  output loads; checks against every part's absolute-maximum and
  recommended limits, regulator ratings (a current limit is only a
  ceiling), discouraged parts and parts without a datasheet.
- Chargers draw the charge current you set (none set: no charging; a
  linear charger draws the larger of that and its load, a power-path
  charger both, up to its input limit). Parts in line on a rail (a load
  switch, a protection IC) count as loads, loops in the power tree are
  reported, rails can carry other loads, and each rail shows its ripple
  budget: how much ripple it can carry before a part on it leaves its
  recommended input range.
- **Block diagram, pins and connections:** each project has a block
  diagram drawn from its connection table: parts with their pins, wires
  labelled with their nets (a bus with junctions where a net joins
  several parts), passives with their values hanging off the pins and
  nets they serve, each ground connection to a small ground symbol, and
  pull-ups pointing to their rail. No wire crosses another net's wire,
  and nothing is drawn on one. Zoom, pan, drag parts and passives,
  export as SVG. A pin table per part lists every pin with what it's
  connected to. The connection table is checked against the parts' pin
  tables: pins that don't exist, ground or supply pins on the wrong net,
  and pins left open that must be connected (supply, ground, exposed
  pads, and pins the datasheet says mustn't float). One designator of a
  BOM line with several ("R1" of "R1,R2") can be connected on its own.
  Agents can draft it all (`set_connections`).
- **BOM import** from CSV/TSV (KiCad, Altium, JLCPCB, LCSC codes): parts
  found in the library or fetched by part number, with JLC availability.
- **Design-review report** as Markdown (Report button, `sheetdiver report`).
- **Calculators:** buck, boost, LDO, MOSFET gate drive, gate bias divider
  and resistor divider, with inputs filled from the datasheets (with page
  references) and project rails, and results saved to the project.

### For AI agents
- A built-in MCP server (streamable HTTP on 127.0.0.1, or `sheetdiver mcp`
  over stdio) with 46 tools: search and read the library, see figures,
  pages and tables as images, record parameters, figure descriptions,
  tables, pins, registers, recommendations and notes, import datasheets and
  BOMs, search the parts catalogue, find alternatives, manage projects, run
  calculators and export reports. Settings switch agents off, make them
  read-only or forbid imports; their changes are marked.
- **Follow agent:** what the agent shows opens in front of you: parts,
  pages, figures, projects with the rail or BOM entry it means
  highlighted, parts compared side by side with the best and worst values
  marked and its pick starred, catalogue candidates, and calculators with
  its inputs. Its explanation sits beside them, with links that open
  parts, pages, figures and sections. It can also walk you through
  several steps (Alt+← / Alt+→).
- **Connect an agent** (the MCP pill) has the setup for Claude Code,
  Claude Desktop, Codex, Cursor, VS Code, Gemini CLI and Windsurf, and
  saves each one's config file. For Claude Desktop it saves an extension
  (`sheetdiver.mcpb`; install it from Settings › Extensions › Advanced
  settings › Install Extension…).
- On Windows a small console program, `sheetdiver-mcp.exe`, is installed
  beside the app for Claude Desktop and other stdio clients; it starts
  SheetDiver when it's closed (an extension setting). Releases also have
  the extension as `sheetdiver_windows_amd64.mcpb` for installer
  installs.

### Apps and packaging
- Desktop app for macOS (universal), Windows and Linux (x64 and ARM64; .deb,
  .rpm, .tar.gz), plus `sheetdiver serve` (headless, the same UI in your
  browser) for every platform. Ten themes shared with CodeDiver.
- Releases ship checksums, an SPDX SBOM, signed build provenance, and the
  third-party notices (`sheetdiver licenses`).

[Unreleased]: https://github.com/aitakemyjob/sheetdiver/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/aitakemyjob/sheetdiver/releases/tag/v0.1.0
