<p align="center">
  <img src="docs/assets/logo.png" alt="SheetDiver" width="200" />
</p>

<h1 align="center">SheetDiver</h1>

<p align="center">
  <b>Your datasheet library, shared with your AI agent.</b>
</p>

<p align="center">
  <a href="https://github.com/aitakemyjob/sheetdiver/actions/workflows/ci.yml"><img src="https://github.com/aitakemyjob/sheetdiver/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <a href="https://github.com/aitakemyjob/sheetdiver/releases/latest"><img src="https://img.shields.io/github/v/release/aitakemyjob/sheetdiver?sort=semver" alt="Latest release" /></a>
  <img src="https://img.shields.io/badge/platforms-macOS%20%7C%20Windows%20%7C%20Linux-4fc3f7" alt="Platforms" />
  <a href="LICENSE"><img src="https://img.shields.io/github/license/aitakemyjob/sheetdiver" alt="MIT license" /></a>
</p>

SheetDiver turns component datasheets into something you and your AI agent
can work with. Drop in a PDF, or give a part number, and it extracts the part
numbers (with orderable variants and family patterns), the section outline,
every captioned graph and reference schematic, the spec tables as min/typ/max
parameters, the pin table and the register-map text, all searchable and
linked back to the page they came from.

Group parts into **projects** for a board, and SheetDiver adds up the current
on every rail (regulators included), checks each rail against every part's
datasheet limits, and runs **design calculators** (buck, boost, LDO, MOSFET
gate drive and gate dividers, resistor dividers) with inputs filled from the
datasheets.

Through its built-in [MCP](https://modelcontextprotocol.io) server, Claude
Code, Cursor, VS Code and other agents can do all of that too. They can look
at the graphs and schematics as images, and record what they verify
(corrected parameters, what a graph shows, recommended inductors, register
maps, notes), so the library gets better every time it's used.

## Features

| | |
|---|---|
| **Import anything** | Drag in PDFs or whole folders, paste a list of URLs (or product pages that link to the PDF) or part numbers, or `sheetdiver import ~/Datasheets TPS563200`. Several import at once, followed in an import queue with retry; re-importing the same file is a no-op. |
| **BOM import** | Load a CSV/KiCad BOM and get a project: each part is found in the library or its datasheet is fetched by part number; parts that can't be fetched join as "no datasheet". |
| **Alternatives** | "What else in my library could replace this?" ranked key by key: ▲ better, = same, ▼ worse, with drop-ins first. |
| **Design reviews** | One click turns a project into a Markdown report: findings, rails and power budget, parts with key parameters and status, calculations. |
| **Datasheet revisions** | Revisions are detected on import; check for newer ones, see which spec rows changed, update in place without losing recorded data. |
| **Favorites and status** | Star parts and mark them preferred, approved, avoid, NRND or EOL. Filter the library and comparisons by them; projects warn about avoid/NRND/EOL parts, and agents are told to prefer your preferred parts. |
| **Part numbers that resolve** | `TPS563200DDCR`, `TLV75733PDBVR`, `MCP1700T-3302E/TT`, `STM32U545VET6Q` and `TPS56x200` all find the right part and datasheet. |
| **Figures** | Every captioned figure is cut out and classified (graph, schematic, waveform, block diagram, pinout, timing, layout, package), with axes from "Y vs X" captions. Describe what a graph shows and it becomes searchable. |
| **Parameters** | Spec tables become min/typ/max rows with conditions, section (absolute max, recommended, electrical…), page, and a canonical key (`vin`, `fsw`, `vref`, `ilim`, `rdson`, `qg`…). Edit any cell; your values win and survive re-extraction. |
| **Pins, sections, registers** | Pin tables, the full section tree with text (pin description, register map, layout guidelines, design procedure…), and structured registers recorded by the agent. |
| **Page viewer** | Pages with figure and table boxes overlaid, the exact region of any parameter highlighted, and drag-to-select to read a region or define a missed figure. |
| **Projects** | BOM with designators, rails and currents; per-rail load with regulator input currents derived from their output loads; checks against absolute-maximum and recommended limits and regulator capacity. |
| **Calculators** | Buck, boost, LDO, gate drive, gate divider and divider: outputs with formulas, checks (minimum on/off time, current limit, thermal), suggestions, and the datasheet page behind every input. |
| **Compare by type** | Pick a component type and see its parts side by side on the parameters that matter for it, sorted and filtered ("VIN ≥ 24 V, IOUT ≥ 3 A, IQ ≤ 1 µA"), with every value linked to its page. |
| **Search** | Full text over parts, pages, sections, figure descriptions, parameters, pins, registers, recommendations and notes. |
| **Themes** | The same themes as CodeDiver: Deep Dive (default), Abyss, Obsidian, Tokyo Night, Nord, Dracula, Gruvbox, Synthwave, Paper and Solarized Light. |

## Install

**macOS / Linux**

```sh
curl -fsSL https://raw.githubusercontent.com/aitakemyjob/sheetdiver/main/scripts/install.sh | sh
```

**Windows** (PowerShell)

```powershell
irm https://raw.githubusercontent.com/aitakemyjob/sheetdiver/main/scripts/install.ps1 | iex
```

Or grab a file from [Releases](https://github.com/aitakemyjob/sheetdiver/releases/latest).
Linux needs WebKitGTK 4.1 (`sudo apt install libwebkit2gtk-4.1-0`). PDF
handling is built in: no Poppler, MuPDF or other tools are needed.

## Quick start

1. Launch SheetDiver and **Import** a datasheet (or `sheetdiver import tps563200`).
2. Connect your agent (the **MCP** pill in the title bar shows these too):

   ```sh
   # Claude Code (user scope: your library is available in every project)
   claude mcp add --transport http --scope user sheetdiver http://127.0.0.1:47480/mcp
   ```

   ```jsonc
   // Cursor (~/.cursor/mcp.json)
   { "mcpServers": { "sheetdiver": { "url": "http://127.0.0.1:47480/mcp" } } }
   // VS Code (.vscode/mcp.json)
   { "servers": { "sheetdiver": { "type": "http", "url": "http://127.0.0.1:47480/mcp" } } }
   // Claude Desktop and other stdio-only clients
   { "mcpServers": { "sheetdiver": { "command": "sheetdiver", "args": ["mcp"] } } }
   ```

3. Ask things like:
   - *"Import the TPS563200 and check its extracted parameters against the electrical characteristics."*
   - *"Look at its efficiency graphs and describe them in SheetDiver."*
   - *"Design a 12 V → 3.3 V, 2 A supply with it: inductor, capacitors, feedback resistors."*
   - *"Put the parts of my sensor board in a project and check every rail against the datasheets."*
   - *"Pick a gate divider for this P-FET switching a 9–24 V rail."*

   With **Follow agent** on, whatever the agent shows opens in front of you.

## Command line

```
sheetdiver                    launch the desktop app
sheetdiver serve              headless; open the same UI in your browser
sheetdiver mcp                stdio MCP bridge to the running app
sheetdiver import FILE|DIR|URL|MPN…   import datasheets (several at a time)
sheetdiver bom FILE [-project N]  build a project from a CSV/KiCad BOM
sheetdiver report PROJECT         print a project's design review (Markdown)
sheetdiver inspect FILE       show what extraction finds in a PDF (nothing is stored)
sheetdiver version
  -port N   MCP/HTTP port (default 47480)   -mpn LIST   (import) set the part numbers
  -fetch=false  (bom) don't download missing datasheets
```

The library (a SQLite database, the PDFs and a render cache) lives in
`~/.local/share/sheetdiver/library` on Linux, `~/Library/Application
Support/SheetDiver/library` on macOS and `%AppData%\SheetDiver\library` on
Windows. Set `SHEETDIVER_LIBRARY` or Settings → Library folder to put it
somewhere you sync.

## How extraction works, and its limits

Extraction is mechanical and deterministic, with no model and no network
access. It is designed to give a good draft with exact page references, which
you or your agent then confirm. It handles outlines, two-column pages,
side-by-side graphs, TI/ST/Microchip/Infineon/AOS-style spec tables and
pin tables, and is checked against a corpus of real datasheets (`make
corpus && make test-corpus`). Dense multi-column tables (large MCU
datasheets) and scanned PDFs extract poorly. The section text and page
images are still there for the agent to read, and anything it records
takes precedence. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Safety model

- The server binds to `127.0.0.1`, rejects non-loopback `Host` headers (DNS
  rebinding) and requires a custom header plus same-origin for the UI API.
- PDFs are parsed by PDFium compiled to WebAssembly, with no filesystem
  access inside the engine.
- Agents' changes are marked `agent` and stay inside the library. Settings →
  Agent access turns agents off, makes them read-only, or forbids imports.
- SheetDiver goes online only to import from a URL or part number.

## Building from source

You need **Go 1.25+** and **Node 22+**; the build scripts install the Wails
CLI if it's missing.

| Platform | Prerequisites | Build |
|---|---|---|
| Linux | `sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev`; optional [`nfpm`](https://nfpm.goreleaser.com) for .deb/.rpm | `scripts/build.sh --package` |
| macOS | Xcode command-line tools | `scripts/build.sh --package` |
| Windows | [WebView2](https://developer.microsoft.com/microsoft-edge/webview2/); optional NSIS | `scripts\build.ps1 -Package` |

```sh
make test        # Go (race) + frontend tests
make dev         # hot-reload development
make headless    # build/bin/sheetdiver-headless (no webview; `serve`)
make corpus      # download the real-datasheet test corpus
make help        # everything else
```

## Releasing

From a clean `main`, `make release VERSION=v0.1.0` runs the tests, then tags
and pushes; the [Release workflow](.github/workflows/release.yml) builds every
platform, writes checksums, an SPDX SBOM and signed build provenance, and
publishes the release. Update [CHANGELOG.md](CHANGELOG.md) first.

## Contributing

Contributions are welcome, especially datasheets that extract badly (open a
[Datasheet extracts badly](https://github.com/aitakemyjob/sheetdiver/issues/new/choose)
issue with a link). Start with [CONTRIBUTING.md](CONTRIBUTING.md) and follow
the [Code of Conduct](CODE_OF_CONDUCT.md). Security reports go to
[SECURITY.md](SECURITY.md).

## Documentation

- [Architecture](docs/ARCHITECTURE.md): packages, the import pipeline, how to extend it
- [MCP tool reference](docs/MCP.md): every tool agents can call
- [Contributing](CONTRIBUTING.md): setup, conventions, the datasheet corpus
- [Roadmap](ROADMAP.md) and [Changelog](CHANGELOG.md)

## License

[MIT](LICENSE). SheetDiver embeds [PDFium](https://pdfium.googlesource.com/pdfium/)
(BSD-3-Clause) through [go-pdfium](https://github.com/klippa-app/go-pdfium) (MIT).
