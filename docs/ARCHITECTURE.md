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
| `internal/pinrole` | What a pin is (supply, ground, pad, input, output…) from a pin table's type column, name or description: the one rule the connection checks, the pin tables, the symbols and extraction share. |
| `internal/library` | The SQLite store (pure-Go `modernc.org/sqlite`, FTS5): datasheets, parts, variants, pages, sections, figures, tables, parameters, pins, registers, recommendations, notes, projects, rails, BOM entries, nets and pin connections, off-BOM blocks and block-diagram positions, calculations, SPICE models, simulations (a test's variations, each with its netlist, models and conditions) and their runs. Part resolution (MPN, variant, prefix, family). Every extracted row has a source: `auto`, `agent` or `user`. |
| `internal/calc` | Pure SI calculators (buck, boost, LDO, gate drive, gate divider, divider) with formulas, checks and E-series helpers. |
| `internal/spice` | Circuit simulation with the user's own ngspice, knowing nothing of projects or the library: finds the program (the path in Settings, the PATH, the installers' usual places) and its version; checks an agent's netlist line by line against an allow list (no `.control`, `.include`, `.lib` files or parameters naming files; a transient's size capped); builds the deck with the part models and SheetDiver's own control block, which runs every analysis and writes each plot to one results file; runs it in a folder of its own with a time limit and a cap on the results' size; reads the raw file (binary or text, real or complex) and the `.meas` values from the log; thins long traces for charts. |
| `internal/core` | The `Service`: imports (path, bytes, URL, product-page links, part-number lookup), part overviews and key parameters, rendering with a cache, calculator input filling from datasheets and project rails (with provenance), project analysis (power tree loads, checks, connection checks against pin tables, pin tables per part, and the block diagram model: `core/diagram.go` and `core/symbol.go` decide what is drawn: each part's symbol (a generated one from its pin table, every pin on a side by its role in number order, connected or not; a box of its connected pins for a block; a transistor by terminal), each passive's standard symbol and which terminal is at which net, tied and ground pins, how each net runs, where each passive hangs, functional groups (a net's wire per group, a labelled stub where it leaves one) and which part each passive supports; the app only places it, in `frontend/src/lib/blockDiagram.ts`, a group at a time inside its frame), agent access switches, activity feed and UI focus/context. |
| `internal/api` | The frontend's JSON method table; one entry point `Invoke(method, args)`. |
| `internal/mcpserver` | MCP tools over `core`, rendered as compact text (plus PNG images for figures and pages); every call is logged to the activity feed. |
| `internal/connect` | What agents need to connect: the stdio command for this install, and the Claude Desktop extension (`.mcpb`). On Windows and Linux the extension carries the console bridge (`server/sheetdiver-mcp[.exe]`), which Claude Desktop starts directly; on macOS it is a manifest and a small Node launcher that runs `sheetdiver mcp`, piping stdio through itself (never inheriting it) and reporting on stderr why SheetDiver stopped. |
| `internal/mcpbridge` | What the stdio bridge needs to reach the app: port lookup (flag, settings file, default), the app's health check, and `Drain`, which answers every request already read before a session ends on stdin EOF (Claude Desktop's version probe sends one request and closes its input). |
| `internal/mcpstdio` | One stdio MCP session, which outlives the app: a switchboard owns the client's session, relays it to the running app (streamable HTTP) or serves the library in-process while the app is closed, and switches between them as the app comes and goes, giving each new backend the client's `initialize` again. Calls run concurrently; one cut off by the app closing is answered with a try-again error, and a message the app never received is sent again. It answers at once and starts nothing, so a client can run copies of it side by side (Claude Desktop starts a short-lived one to probe the protocol version). Protocol messages go to stdout only; diagnostics go to stderr, and each session is logged (how it was started, each message's method and id, how it ended) to `mcp.log` in the user's cache folder, whatever the client does with stderr. |
| `cmd/sheetdiver-mcp` | `internal/mcpstdio` as a console program with no UI, for stdio clients. Windows needs it: the desktop app is a windowed (`-H windowsgui`) program, which a client can't run as a stdio server there. The Windows installer and the Linux packages put it beside the app, and the Windows and Linux extensions carry their own copy (on Linux it keeps Node and the app's GTK libraries out of the session). |
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
so no PDF library runs in the browser. The block diagram is laid out by
ELK (elkjs, loaded on first use): `lib/blockDiagram.ts` sizes each
symbol from its pins (a body with a pin line out to each pin end, where
the wire attaches) and turns the backend's `Diagram` into a graph per
group (symbols with their pin ends as fixed ports, passives as small
nodes, the name of a net leaving the group as a port label so its stub
is straight, a net as edges from the pin that drives it) and ELK's
result back into the drawing; dragged parts are hints to ELK's
interactive crossing minimisation, which keeps their order in their
layer and routes the wires again. Which nets become wires and which
become labels follows schematic practice, decided per net in `graph()`:
grounds are symbols at each pin; a rail nothing in the group drives
isn't wired between its parts: each pin wires what hangs off it by its
part, the wire named, and a pin with nothing gets a named stub (between
just two parts with nothing hanging off it, one wire does); a signal
reaching three parts or spanning more than a column is a net label at
each pin; everything else (a crystal with its load capacitors, a series
resistor, a pin's own pull-up, a button and its resistor) is wired at
the pin. One rule places and wires a passive: it gets a wire from what
it's placed by, an IC pin, a connector pin or the previous passive in a
chain, on that pin's side of the part. Which ends of a net are wired to
which, and which get a named stub, is decided by `lib/diagram/wiring.ts`:
a chain of rules (an undriven rail, a far signal, the wired net) over
facts about the net alone, each unit-tested on small contexts; `graph()`
only turns the plan into ELK edges. Which parts form a subgroup is the
backend's decision (`internal/core/subgroups.go`, a rule chain too:
named, chain, rail), carried in `Diagram.subgroups` so the drawing and
the agent's text agree. Subgroups are laid out first, each as one unit
that the group's graph then places as a single node (`clusterUnits`):
the parts hanging off one rail by one part, with the series part leading
to it, go on a bus named after the net in a faint frame, on the side of
the part their pin is on; parts named into a subgroup (`cluster` on an
assignment) are laid out by ELK on their own, everything inside wired,
with a port on the frame for each net reaching it from outside.
Everything is on a 10 px grid (`GRID`): symbol sizes, port offsets and
ELK's spacings are multiples of it, and ELK's result is snapped to it,
which keeps wires straight since points that shared a line still do.
A part asked for a column and row (`want`) is given them through ELK's
interactive layering and crossing minimisation: `lib/diagram/cells.ts`
turns the first layout's columns and the places asked for into the
coordinates the second pass reads, and the drawn layout's columns back
into each node's cell, which the app reports (`diagram.drawn`) for
`get_connections`; `keepSides` keeps a place asked for on its side of
what the node is wired to. The report carries what the layout checks
in `lib/diagram/lint.ts` find on the drawing (each check a rule of its
own: a wire round a part, a wire through one, overlaps, crossings),
built by `lib/diagram/report.ts`. For `get_diagram_image` the backend
publishes a `snapshot` request on the event bus (`core/snapshot.go`)
and waits for the app showing that project's diagram to answer
(`diagram.snapshot`): `lib/diagram/snapshot.ts` crops the shown SVG to
the drawing or a group's frame, resolves the theme's variables and
draws it on a canvas as PNG.
Groups are placed by `lib/diagram/placement.ts`: frames in columns in
power-flow order, a group stacked under the one it names as `below`.
`lib/symbols.ts` holds the standard
symbols' geometry (two-terminals down the y axis, transistors in a box
with their terminals on its border); `views/project/BlockDiagram.tsx`
draws it all as SVG and lets the user hide a generated symbol's
unconnected pins. Agent `focus` events
open tabs when **Follow agent** is on; the UI reports what is open through
`ui.context` for `get_ui_context`.

## Adding things

- **An MCP tool:** an args struct and `addTool` call in `internal/mcpserver`,
  logic in `core`, an entry in `docs/MCP.md` (a test enforces it) and a case
  in `mcpserver_test.go`.
- **A parameter key:** add it to `extract.Keys`, a rule in `keyRules` and its
  unit in `keyUnits`; use it in `core.fills` if a calculator needs it.
- **A calculator:** see CONTRIBUTING.md.
- **A manufacturer's lookup URL:** `datasheetURLs` in `internal/core/importer.go`.
- **A corpus datasheet:** `testdata/corpus/manifest.json`.
