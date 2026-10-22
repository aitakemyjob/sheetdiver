# Architecture

SheetDiver is one Go binary, built like [CodeDiver](https://github.com/aitakemyjob/codediver).
The desktop shell (Wails), the browser mode and the MCP server are thin
adapters over a single application service, so the UI and agents see
exactly the same library through the same code.

```
            ┌──────────────── frontend (React + TS) ────────────────┐
            │ views: part · page · figure · project · calc · search  │
            │ lib/transport.ts ── Wails bindings │ HTTP + SSE        │
            └───────────────┬────────────────────┴─────────┬─────────┘
                            │ Invoke(method, json)          │  /api/page.png, /api/upload…
   gui.go (Wails) ──────────┤                               │
   internal/server ─────────┴─ /api/invoke · /api/events ───┘
                            │
                     internal/api  (JSON method table)        internal/mcpserver (MCP tools)
                            └──────────────┬──────────────────────────┘
                                     internal/core  (Service)
          ┌──────────────┬───────────────┼───────────────┬──────────────┬────────────┐
     pdf (PDFium/wasm)   extract (pure)  library (SQLite)  calc (pure)   events (bus)  config
```

## Import pipeline

Imports run as **jobs** (`internal/core/jobs.go`): each one is queued,
downloads its PDF, waits for an extraction slot, then reads, analyses and
saves, publishing its progress as `import` events. The PDF engine keeps a
pool of PDFium instances (up to four, one per CPU), so that many
documents are read at once; saving is serialized behind one lock. Folder,
list, BOM and agent imports all go through the same queue.

1. **`internal/pdf`** opens the PDF bytes in PDFium compiled to WebAssembly
   (go-pdfium on wazero: no cgo, and the engine has no filesystem access).
   It returns, per page, text spans built from per-character positions,
   the bounds of every graphic object, an ink map (a 72-DPI one-bit render)
   for pages that mention figures, plus bookmarks and metadata. All
   coordinates are points from the top-left.
2. **`internal/extract`** is pure and deterministic. It groups spans into
   rows and lines, detects two-column pages for reading order, builds the
   section tree from the outline (or headings), and finds:
   - **figures:** caption lines are paired with the cluster of graphics
     above or below them in their column (side-by-side captions split the
     page into columns); regions are clipped at neighbouring captions and
     trimmed to ink, because PDFs often report bogus object bounds;
   - **spec tables:** a MIN/TYP/MAX header defines columns by position;
     text-only rows (wrapped names, multi-line conditions) attach to the
     nearest numeric row; units shared by several rows are filled in; a
     canonical **key** (`vin`, `fsw`, `vref`, `rdson`, …) is assigned only
     when the unit matches;
   - **spec-table groups:** horizontal rules that cross the parameter-name
     column separate parameters, so a name centred across several condition
     rows applies to all of them; typ printed half a line above min/max is
     merged back into one row; "–3% / 0.511 / +3%" becomes a tolerance;
   - **other tables:** every captioned table keeps its region (rendered as
     an image on demand). Tables that aren't a plain grid (selection
     matrices, grouped rows, multi-row headers) are flagged *irregular*:
     their image is the reference until the agent transcribes them with
     `record_table`. Regular "recommended inductors/capacitors" tables
     become recommendations;
   - **pins**, **part numbers** (scored from title, large first-page text
     and running headers), **family patterns** (`STM32U545xx`), **orderable
     variants** (tokens extending a base part, incl. voltage codes inserted
     before a trailing letter), **manufacturer** and **category**.
3. **`internal/core`** copies the PDF into the library and stores the result.

Extraction is a draft. The design assumption is that the connected agent
reviews it: it reads pages and figure images through MCP and records
corrections, which are kept and preferred.

## Packages

| Package | Responsibility |
|---|---|
| `internal/config` | Settings (`os.UserConfigDir()/sheetdiver/config.json`, override with `SHEETDIVER_CONFIG`) and the library location (`SHEETDIVER_LIBRARY`). |
| `internal/pdf` | PDFium wrapper: text spans, graphic bounds, ink maps, bookmarks, metadata, page/region rendering. |
| `internal/extract` | Datasheet structure from a `pdf.Document`: sections, figures, tables, parameters, pins, part numbers, families, variants. |
| `internal/library` | The SQLite store (pure-Go `modernc.org/sqlite`, FTS5): datasheets, parts, variants, pages, sections, figures, tables, parameters, pins, registers, recommendations, notes, projects, rails, BOM entries, calculations. Part resolution (MPN, variant, prefix, family). Every extracted row has a source: `auto`, `agent` or `user`. |
| `internal/calc` | Pure SI calculators (buck, boost, LDO, gate drive, gate divider, divider) with formulas, checks and E-series helpers. |
| `internal/core` | The `Service`: imports (path, bytes, URL, product-page links, part-number lookup), part overviews and key parameters, rendering with a cache, calculator input filling from datasheets and project rails (with provenance), project analysis (power tree loads, checks), agent access switches, activity feed and UI focus/context. |
| `internal/api` | The frontend's JSON method table; one entry point `Invoke(method, args)`. |
| `internal/mcpserver` | MCP tools over `core`, rendered as compact text (plus PNG images for figures and pages); every call is logged to the activity feed. |
| `internal/connect` | What agents need to connect: the stdio command for this install, and the Claude Desktop extension (`.mcpb`). On Windows the extension carries the console bridge (`server/sheetdiver-mcp.exe`); elsewhere it is a manifest and a small Node launcher that runs `sheetdiver mcp`. |
| `internal/mcpbridge` | The stdio↔HTTP MCP bridge: port lookup (flag, settings file, default), relaying to the app's `/mcp`, and, for the console bridge, a small state machine that starts a closed app (found beside the bridge or from the installer's registry entry) or answers with why it can't be reached. Logs go to stderr only. |
| `cmd/sheetdiver-mcp` | The bridge as a console program with no UI, for stdio clients. Windows needs it: the desktop app is a windowed (`-H windowsgui`) program, which a client can't run as a stdio server there. The installer puts it beside `sheetdiver.exe`. |
| `internal/server` | Loopback HTTP: `/mcp`, `/healthz`, and in browser mode the UI, `/api/invoke`, `/api/events` (SSE) and the binary endpoints (`page.png`, `figure.png`, `datasheet.pdf`, `upload`). |
| `main.go`, `gui.go`, `bridge.go`, `inspect.go` | CLI (`serve`, `mcp`, `import`, `inspect`), Wails desktop shell (`!nogui`), the `mcp` subcommand (the bridge, or the library served over stdio while the app is closed). |

## Design principles

- **One service, many adapters.** Logic lives in `core`; the UI API and MCP
  tools only translate.
- **Recorded beats extracted.** Re-extraction replaces only `auto` rows,
  keeps figure IDs stable (notes refer to them), and skips automatic
  parameters whose key the agent or user already recorded.
- **Provenance everywhere.** Parameters and figures keep their page and
  region; calculator inputs filled from a datasheet say where from.
- **Pure where possible.** Extraction, calculators and tab state are pure
  functions with unit tests; the corpus test checks extraction on real
  datasheets that are never committed.
- **Local and explicit.** The server binds to 127.0.0.1, rejects foreign
  Host headers and cross-origin calls; network access happens only when you
  or an agent import from a URL or part number.

## Frontend

React 19 + TypeScript + Vite, zustand for state (`lib/store.ts`), pure tab
helpers (`lib/tabs.ts`). Themes are CodeDiver's palettes driving CSS
variables (`lib/themes.ts`). Pages and figures are rendered by the backend
to PNG (`/api/page.png`, `/api/figure.png`) and overlaid with HTML boxes,
so no PDF library runs in the browser. Agent `focus` events open tabs when
**Follow agent** is on; the UI reports what is open through `ui.context`
for `get_ui_context`.

## Adding things

- **An MCP tool:** an args struct and `addTool` call in `internal/mcpserver`,
  logic in `core`, an entry in `docs/MCP.md` (a test enforces it) and a case
  in `mcpserver_test.go`.
- **A parameter key:** add it to `extract.Keys`, a rule in `keyRules` and its
  unit in `keyUnits`; use it in `core.fills` if a calculator needs it.
- **A calculator:** see CONTRIBUTING.md.
- **A manufacturer's lookup URL:** `datasheetURLs` in `internal/core/importer.go`.
- **A corpus datasheet:** `testdata/corpus/manifest.json`.
