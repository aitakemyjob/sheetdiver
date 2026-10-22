# Changelog

Notable changes to SheetDiver. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org). Each
[GitHub release](https://github.com/aitakemyjob/sheetdiver/releases) also lists
every merged pull request.

## [Unreleased]

### Added
- **Package filter in the parts catalogue:** every category can be filtered
  by package, picking several at once (SOT-23, SOT-23-3, SOT-23(TO-236)…),
  alongside its parameters. Text filters (package, channel type, topology…)
  take several values. Number parameters take = as well as ≥ and ≤
  (Resistance = 10k, Output Voltage = 3.3V): values compare by value within
  0.1% (10k matches 10.0kΩ, not 10.2kΩ), ranges match when they contain
  it, and the category's common values are suggested. Each filter's list
  of values and ranges follows the
  other filters, so after "V<sub>DS</sub> ≥ 30 V" the package list shows only
  packages that still have matching parts, with their counts. Agents get
  the same (`Package = SOT-23|SOT-23-3`, faceted `catalog_parameters`).
- **Delete from the library:** the trash button on a part opens a
  confirmation that lists what goes: the datasheet with all its parts,
  what you and agents recorded for it, notes, and the projects that use
  the parts (kept there without a datasheet, or removed with them, your
  choice). Agents can't delete.
- **Open a catalogue part at LCSC or JLCPCB:** buttons in the part's
  details pane, and its LCSC code in the results is a link. The desktop
  app opens your browser; only part pages built from the LCSC code can be
  opened.

- **Claude Desktop extension and config files for other agents:** the
  connect dialog (the MCP pill) saves a Claude Desktop extension
  (`sheetdiver.mcpb`; install it from Claude Desktop's Settings › Extensions
  › Advanced settings › Install Extension…), and each agent's config file
  (`.mcp.json`, Cursor and VS Code `mcp.json`, Codex `config.toml`, Gemini
  CLI `settings.json`, Windsurf `mcp_config.json`). Commands and files that
  launch SheetDiver use this copy's full path, since `sheetdiver` isn't on
  PATH on Windows or macOS. Codex, Gemini CLI and Windsurf are new in the
  list. (#53)

### Fixed
- **Windows: the Claude Desktop extension couldn't connect.** The app is a
  windowed program, which Claude Desktop can't run as a stdio server on
  Windows. Windows now has a small console bridge, `sheetdiver-mcp.exe`,
  installed beside the app (and in the zip), and the extension carries its
  own copy instead of a Node launcher, so it no longer depends on where
  SheetDiver is installed. When the app is closed it starts it (the
  extension's *Start SheetDiver* setting), or tells Claude to open it.
  The port comes from the extension's settings, else SheetDiver's. Codex
  and other stdio clients on Windows are pointed at the bridge too. Each
  release has the extension as `sheetdiver_windows_amd64.mcpb` (for the
  installer; with the zip, save it from the app). (#60)
- **Windows: importing datasheets failed** with "PDF engine busy: could not
  instantiate webassembly module: GetFileType /dev/stdout: The handle is
  invalid". The desktop app has no console, and the PDF engine was given
  its (invalid) output handles. (#51)
- **Downloading the parts catalogue often failed** with "the catalogue was
  republished during the download". A publication's files are dated a
  second or so apart, and the download required identical dates. It now
  allows for that, and still restarts if the dataset really is
  republished while downloading. (#52)
- **A rail current limit of 0 A** hid the regulator's rated output, so the
  rail's load wasn't checked. Zero now means no limit. (#50)

## [0.1.0] - 2026-10-19

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
  ratings stated only on the first page. A corpus of 18 real datasheets
  guards it (`make corpus && make test-corpus`); `sheetdiver inspect`
  shows what it finds.
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

### Parts catalogue (JLCPCB / LCSC)
- One click downloads the open [jlcparts](https://github.com/yaqwsx/jlcparts)
  dataset (about 2.5 million parts, republished from JLCPCB three times a
  day; about 200 MB), searched offline with a reminder to refresh it.
- Browse by category, search by part number or specs, and filter on each
  category's own parameters (Drain to Source Voltage ≥ 30V, RDS(on) ≤ 10mΩ,
  Type = N-Channel; supply ranges compare as "accepts"), with stock, price,
  JLC basic/extended library and the parameters as sortable columns.
- Tick parts to import their datasheets. Part-number lookups and BOM import
  fall back to its datasheet links.

### Projects and design
- **Projects** for a board: BOM with designators, rails and currents;
  per-rail load including regulator input currents derived from their
  output loads; checks against every part's absolute-maximum and
  recommended limits, regulator ratings (a current limit is only a
  ceiling), discouraged parts and parts without a datasheet.
- **BOM import** from CSV/TSV (KiCad, Altium, JLCPCB, LCSC codes): parts
  found in the library or fetched by part number, with JLC availability.
- **Design-review report** as Markdown (Report button, `sheetdiver report`).
- **Calculators:** buck, boost, LDO, MOSFET gate drive, gate bias divider
  and resistor divider, with inputs filled from the datasheets (with page
  references) and project rails, and results saved to the project.

### For AI agents
- A built-in MCP server (streamable HTTP on 127.0.0.1, or `sheetdiver mcp`
  over stdio) with 43 tools: search and read the library, see figures,
  pages and tables as images, record parameters, figure descriptions,
  tables, pins, registers, recommendations and notes, import datasheets and
  BOMs, search the parts catalogue, find alternatives, manage projects, run
  calculators and export reports. Settings switch agents off, make them
  read-only or forbid imports; their changes are marked.
- Follow agent: what the agent shows opens in front of you.

### Apps and packaging
- Desktop app for macOS (universal), Windows and Linux (x64 and ARM64; .deb,
  .rpm, .tar.gz), plus `sheetdiver serve` (headless, the same UI in your
  browser) for every platform. Ten themes shared with CodeDiver.
- Releases ship checksums, an SPDX SBOM, signed build provenance, and the
  third-party notices (`sheetdiver licenses`).

[Unreleased]: https://github.com/aitakemyjob/sheetdiver/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/aitakemyjob/sheetdiver/releases/tag/v0.1.0
