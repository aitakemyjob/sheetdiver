# Changelog

Notable changes to SheetDiver. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org). Each
[GitHub release](https://github.com/aitakemyjob/sheetdiver/releases) also lists
every merged pull request.

## [Unreleased]

### Changed
- A regulator without an output-current rating no longer uses its current
  limit as its capacity: the rail shows "capacity unknown" with the limit
  as a ceiling (a load above it is still an error).
- Zero-gate-voltage drain current (`idss`) and on-state drain current
  (`id_on`) are no longer filed as the continuous drain current `id`.
- CSV and report saving use the desktop save dialog.
- Characters set in Symbol fonts are read as Greek (µ, Ω, θ…): Office-made
  PDFs such as AOS MOSFET datasheets no longer read 1 µA as 1 mA.
- Regulators whose spec tables have no output-current row get their rating
  from the first page (per part when listed, else the lowest stated
  rating; boost titles are skipped since they quote the switch limit).
- Ground current is filed as `iq`, short-circuit current as `isc`, and
  input currents no longer pose as `iout`.
- Datasheets read by an older extractor are re-extracted in the background
  at startup (recorded data is kept), so extraction fixes reach existing
  libraries.

### Added
- **Alternatives:** a part's Alternatives tab (MCP `find_alternatives`)
  ranks library parts of the same or a related type key by key in the
  direction that matters (more current, less quiescent current, input
  range covering the original's, matching reference); drop-ins first,
  preferred parts win ties.
- **Design-review report** for a project (Report button, `sheetdiver
  report`, MCP `export_project_report`): findings, rails with load and
  capacity, parts with status and key parameters, calculations, notes, as
  Markdown.
- **Datasheet revisions:** the revision is detected on import (TI, ST,
  ADI/Maxim, Microchip, Diodes, AOS styles). Check one datasheet or the
  whole library for a newer revision, see which spec rows changed, and
  update in place; recorded data and projects are kept (MCP
  `check_datasheet_updates`, `apply_datasheet_update`).
- **Batch import:** whole folders (recursively), lists of part numbers or
  URLs, and many dropped files at once, extracted in parallel on a pool of
  PDF engines. An import queue shows progress, results and failures, with
  retry; `import_datasheets` does the same for agents.
- **BOM import** (`sheetdiver bom`, Import → BOM, MCP `import_bom`): CSV/TSV
  from KiCad, Altium, JLCPCB and similar; designator ranges expanded;
  parts resolved through orderable codes or fetched by part number;
  unfetchable parts kept as "no datasheet" placeholders and flagged in
  project checks.
- **Favorites and part status** (preferred, approved, avoid, NRND, EOL):
  stars and badges in the library, part view and comparisons; filters in
  the library, comparisons, `list_parts` and `compare_parts`; project
  checks warn about discouraged parts.
- **Import** datasheet PDFs by drag-and-drop, file picker, URL (including
  product pages that link to the PDF), part number (TI, ST, ADI, onsemi,
  Nexperia and Diodes datasheet URLs), or `sheetdiver import`. PDFs are
  read with PDFium compiled to WebAssembly: no cgo, no external tools.
- **Part numbers** with families and variants: base parts, family patterns
  (`TPS56x200`, `STM32U545xx`), orderable codes (`TPS563200DDCR`,
  `TLV75733PDBVR`, `MCP1700T-3302E/TT`) and unlisted ordering codes
  resolved by prefix (`STM32U545VET6Q` → `STM32U545VE`).
- **Extraction:** section tree (from the PDF outline or headings, with
  two-column reading order), captioned figures cut out and classified
  (graph, schematic, waveform, block diagram, pinout, timing, layout,
  package) with axes parsed from "Y vs X" captions, spec tables to
  min/typ/max parameters with canonical keys, pin tables, manufacturer and
  category.
- **Library:** SQLite with full-text search over parts, pages, sections,
  figures, parameters, pins, registers, recommendations and notes. Every row
  records whether it came from the extractor, the agent or you; recorded
  data wins and survives re-extraction.
- **UI:** part overview with key parameters, editable parameter table,
  figure gallery and editor, pins, sections, registers, tables, notes and
  variants; page viewer with figure/table overlays, highlights and
  region selection; projects; calculators; search; themes shared with
  CodeDiver.
- **Projects:** BOM with designators, rails and load currents; per-rail
  load including regulator input currents derived from their output
  loads; checks against each part's absolute-maximum and recommended
  limits and regulator capacity.
- **Calculators:** buck, boost, LDO, MOSFET gate drive, gate bias divider
  and resistor divider, with inputs filled from the datasheet (with page
  references) and project rails.
- **MCP server** with 32 tools to search and read the library, look at
  figures and pages as images, record parameters, figure descriptions,
  pins, registers, recommended components and notes, manage projects and
  run calculators. Settings switch agents off, make them read-only, or
  forbid imports.
- **Component types:** a Types panel lists the library's categories;
  each opens a side-by-side comparison of its parts on the type's key
  parameters (bucks: VIN, VOUT, IOUT, fSW, ILIM, IQ, RDS(on)…; MOSFETs:
  VDS, ID, RDS(on), VGS(th), Qg…), with sortable columns, ≥/≤ filters that
  take units ("24 V", "1µ"), a column picker, text search, CSV export,
  and cells that open the datasheet page. Agents get `list_categories`
  and `compare_parts` for picking a part from the library.
- **Tables as images:** every table renders as an image (`get_tables`,
  the Tables tab). Selection matrices and other irregular tables are
  flagged, and the agent saves a clean transcription with `record_table`
  (searchable, kept on re-extraction). Recommended inductor and capacitor
  tables become recommendations automatically.
- **Spec tables:** parameter names centred across several condition rows
  (TI) are applied to every row, using the table's rules; typ printed
  half a line from min/max is merged; ±% limits become a tolerance around
  typ. Current limits are told apart: `ilim` (normal operation),
  `ilim_startup` and `ilim_in` (average input).
- **Pin tables with package columns** (Maxim/ADI "20-BUMP WLP" /
  "20-PIN TQFN"): each pin is recorded per package, multi-pin entries
  ("A3, B3") stay intact, and subscripted names read as one word (VSYS,
  I2C).
- Punctuation no longer splits off the text line (stray "," rows in pin
  tables and ", ," in conditions).
- The **PDF** button opens the datasheet in your PDF viewer in the
  desktop app (the webview can't open tabs); the page viewer has one too.
- The SheetDiver badge (`sheetdiver.png`) as app icon and logo.
- `sheetdiver inspect` prints what extraction finds in a PDF; a corpus of
  real datasheets (`make corpus`, `make test-corpus`) guards extraction.
