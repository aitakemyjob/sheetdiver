# Changelog

Notable changes to SheetDiver. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org). Each
[GitHub release](https://github.com/aitakemyjob/sheetdiver/releases) also lists
every merged pull request.

## [Unreleased]

### Added
- **Functional groups on the block diagram:** put parts in groups (Power,
  MCU, Sensors) in the Connections tab, or have your agent do it. Each
  group is drawn in its own frame with its description and notes. Wires
  stay inside a group; a net that also runs in another group ends in a
  stub with its name, so a rail shared by the whole board no longer
  draws wires across it. A board without groups is drawn as before; the
  report's Mermaid chart shows each group as a subgraph. (#72)
- **Passives drawn by the part they support:** say which part a passive
  supports (a decoupling capacitor and its IC), and it hangs by that
  part's pin and is drawn in that part's group. Alike passives on one
  rail are only merged when they serve the same part, so C4 and C5 for
  two ICs on 3V3 no longer clump together at the regulator. Left unset,
  its support is guessed: the IC on its nets (over a connector or
  module), and of several, the one it feeds rather than the one driving
  the net (a bulk capacitor between an eFuse and a regulator goes by the
  regulator's VIN); a protection diode goes by its connector. The app
  shows a guess as "auto (U2)", agents as "(supports U2, guessed)", and
  confirms each change. (#71)

### Changed
- **The block diagram is laid out by ELK** (the Eclipse Layout Kernel,
  elkjs) instead of SheetDiver's own router, which found a new way to
  tangle on each new board. Wires are routed around every part, a net's
  branches merge into a bus with junctions, labels go where they fit, and
  a pin's order on its side follows where its wire goes. A net leaving a
  group is a short stub straight out of its pin, named; a net wired in
  the group is named on its wire. Dragging a part now tells the layout
  which order you want it in among the parts beside it (left to right
  stays with the flow, and the wires are routed again) rather than
  pinning it exactly where it was dropped. *Auto layout* still forgets
  the dragged positions.
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
- **Block diagram, from a tester's USB-C board**: a part's second pin of
  the same name on a net (a USB-C connector's two VBUS pins, a
  regulator's two IN and OUT pins) got a wire of its own, drawn as a
  zero-length stub with the net's name over the pin; it is now tied to
  the first like any other. A net reaching another group (a crystal's
  pins to the MCU) was put on the bottom of its box with no wire or
  label; it now leaves a side as a labelled stub. Stubs leaving a box
  are one length, sized to their labels, instead of each longer than
  the one below.
- **Imports named a part by LCSC's file id** ("2304140030") when the PDF
  itself named no part clearly: a distributor's file name
  (`2304140030_STMicroelectronics-STM32G031K8T6_C432211.pdf`) now gives
  the part number, without the manufacturer and file ids.
- **A datasheet title's ratings were taken for a part** ("1.8A/3.1A" from
  MAX77827's). The extractor version is now 5, so SheetDiver offers to
  re-extract; parts already imported under a wrong name keep it until
  renamed or removed.
- **Block diagram wiring on boards in groups**: wires ran through a part
  (a header's pin faced a part in another group, so its wire crossed the
  header), labels sat on a part's pin names when the piece of wire under
  them was too short, and wires of two nets met, looking shorted. Pins
  now face only parts in their own group; a label goes on a piece of its
  wire it fits; a rail reaches a part beyond the next column by a
  labelled stub; wires that go round the columns between their ends try
  over and under and take the way that crosses least; a gap leaves room
  for the capacitors hanging beside a part; and a passive with no room on
  its wire hangs beside the wire's vertical run instead of across
  another net.
- **A passive set to support a part is drawn by it even in series** (PA5
  → R5 → LED: R5 hung by the MCU, on its other end).
- **A debug header was drawn as a supply.** A connector no longer counts
  as bringing in a rail that a part on the board makes, or that it only
  senses (VTref, VREF, VTARGET).
- **VSS and VSSA were flagged as supply pins on ground**: a ground pin is
  never a supply pin, whatever its type or name.
- **No more "not connected to a rail" or "no load current" warnings for
  parts that draw no current**: LEDs, switches, crystals, connectors,
  transistors and passives, by category or designator (D, SW, Y, J…).
  Such categories ("LED", "Tactile switch", "Pin header") are also drawn
  as what they are instead of as ICs or connectors.
- **The Claude Desktop extension stopped working when SheetDiver was
  closed or restarted**, until it was disabled and enabled again. A stdio
  session now outlives the app: it serves the library itself while the
  app is closed and goes back to the app once it's running again, without
  the client reconnecting. A request that was running when the app closed
  gets an error saying to try it again, instead of hanging. A long request
  no longer holds up the others in the same session.
- **Linux: the Claude Desktop extension couldn't connect.** It ran
  SheetDiver through a Node launcher, which on some setups exited before
  SheetDiver answered, leaving nothing in Claude Desktop's log. The Linux
  extension now carries `sheetdiver-mcp`, a small static program that
  Claude Desktop starts directly, as on Windows: no Node, no GTK, and it
  works with any install. The `.deb`, `.rpm` and `.tar.gz` install it as
  `/usr/bin/sheetdiver-mcp`, and each Linux release has the extension as
  `sheetdiver_linux_<arch>.mcpb`. Save the extension again from the
  connect dialog (or use the release's) and reinstall it.
- **Claude Desktop's version probe broke the connection while the app was
  closed.** Claude Desktop can probe a server for the newest MCP protocol
  and then start the session on the same connection. SheetDiver answered
  the probe in the new protocol and then refused the session
  ("duplicate initialize"). Every path now speaks the protocol versions
  with the `initialize` handshake, up to 2025-11-25, as the app's HTTP
  endpoint already did.
- **MCP sessions are logged.** Each stdio session (the Claude Desktop
  extension, `sheetdiver mcp`) is recorded in `mcp.log` in your cache
  folder (`~/.cache/sheetdiver/mcp.log` on Linux): how it was started,
  each request's method and id (not their contents), and how it ended,
  so a failed connection says why.
- **macOS: the Claude Desktop extension exited at once ("version
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
