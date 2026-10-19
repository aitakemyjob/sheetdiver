# Changelog

Notable changes to SheetDiver. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org). Each
[GitHub release](https://github.com/aitakemyjob/sheetdiver/releases) also lists
every merged pull request.

## [Unreleased]

### Added
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
- `sheetdiver inspect` prints what extraction finds in a PDF; a corpus of
  real datasheets (`make corpus`, `make test-corpus`) guards extraction.
