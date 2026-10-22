# MCP tool reference

SheetDiver serves the [Model Context Protocol](https://modelcontextprotocol.io)
at `http://127.0.0.1:47480/mcp` (streamable HTTP), and over stdio through
`sheetdiver mcp` (on Windows, `sheetdiver-mcp.exe`, installed beside the
app). Every call appears in the app's agent activity feed.

Conventions:

- A **part** argument accepts a base part number (`TPS563200`), an orderable
  code (`TPS563200DDCR`), an unlisted ordering code that extends a known part
  (`STM32U545VET6Q` → `STM32U545VE`), a family pattern (`TPS56x200`) or an id.
- Pages are **1-based**. Regions are in PDF points from the top-left of the
  page (US Letter is 612 × 792).
- Calculator inputs and outputs are **SI** (V, A, Hz, s, H, F, Ω, C).
- Data the agent records is marked `agent`, kept when a datasheet is
  re-extracted, and preferred over automatic extraction.
- The user can switch agents off, make them read-only, or forbid imports in
  Settings → Agent access.

## Library

| Tool | What it does |
|---|---|
| `library_overview` | Datasheets, parts by category, projects, and what the user has open. |
| `get_ui_context` | The part, page, figure or project the user is looking at, and any selected text. |
| `search` | Full-text search across parts, page text, sections, figure captions and descriptions, parameters, pins, registers, recommendations and notes. |
| `list_categories` | Component types with part counts and the key parameters compared for each. |
| `compare_parts` | (Optionally only starred parts or one status.) One type's parts side by side on key parameters (min/typ/max in SI), filtered by requirements such as `vin >= 24 V`, `iout >= 3 A`, `iq <= 1 µA` and sorted. A `>=` filter tests the highest the part can do (max), `<=` tests typ. |
| `list_parts` | Parts filtered by part number/variant/description, category or manufacturer. |
| `get_part` | Overview: datasheet, sibling parts, orderable variants, best value per key parameter (with page), sections, figure counts, recommended components, projects, notes. |
| `get_parameters` | Spec parameters (min/typ/max, unit, conditions, section, page), filtered by key, section or text. |
| `get_section` | A section's text by kind (`pins`, `registers`, `layout`, `design_procedure`, `component_selection`, `typical_application`, …), title or number; without one, the outline. The text is formatted as light markdown: paragraphs separated by blank lines, `### ` sub-headings and `• ` list items. |
| `read_pages` | Plain text of up to 10 pages. |
| `get_page_image` | A page or a region of it as a PNG image. |
| `list_figures` | Figures with number, page, kind (`graph`, `schematic`, `waveform`, `block_diagram`, `pinout`, `timing`, `layout`, `package`), caption, axes and recorded description. |
| `get_figure` | A figure as an image, with what is known about it and the text inside it. |
| `get_tables` | Detected tables, or one table as an image with its cells. Tables flagged IRREGULAR (matrices, grouped rows, multi-row headers) have unreliable cells; read the image. |
| `get_pins` | The pin table. |
| `get_registers` | Recorded registers and the register-map sections to read from. |
| `show` | Put something on the user's screen while discussing it: a part, a page (optionally highlighting a region), a figure, a section, a project (highlighting a rail or BOM entry; `tab` `diagram`, `pins` or `connections` opens its block diagram, pin tables or connection table, highlighting a net, part or block), search results, parts compared side by side (`compare`: parts, keys, a note per part, the recommended one), catalogue candidates (`catalog`: LCSC codes or a search, the recommended one) or a filled-in calculator (`calc`). `explain` adds the agent's reasoning in markdown beside the view; links like `[p3](#page/TPS63900/3)`, `#figure/PART/5`, `#part/PART`, `#section/PART/Title` and `#project/Name` open what they name. |
| `show_walkthrough` | Walk the user through something step by step: each step shows something as `show` does, with a title and an explanation; the user clicks through them. |

## Recording what the agent learns

| Tool | What it does |
|---|---|
| `import_datasheet` | Download and import a datasheet: by `part_number` (tries TI, ST, ADI, onsemi, Nexperia and Diodes datasheet URLs), from a `url` (a PDF, or a product page linking to one), a local `path`, or `data_base64` for a PDF the agent fetched itself because the site blocks scripted downloads. `reextract` redoes extraction. |
| `find_alternatives` | Parts in the library that could replace a part: same or related type, compared key by key in the direction that matters (▲ better, = same, ▼ worse, ? unknown); drop-ins first, preferred parts win ties. |
| `export_project_report` | A project as a Markdown design-review document: findings, rails with load and capacity, parts with status and key parameters, calculations, notes. |
| `check_datasheet_updates` | Download a part's datasheet again (or all of them) and report a newer revision with the spec rows that changed. Needs import access. |
| `apply_datasheet_update` | Replace a datasheet with the newer revision just found: same datasheet, parts and projects; extracted data replaced, recorded data kept. Needs import access. |
| `catalog_parameters` | The parts catalogue's categories (with part and in-stock counts), or one category's parameters: Package, numeric (unit and range) or text (common values), for `search_catalog` filters. With filters, each parameter is summarized over the parts that pass the other filters (the packages still available after `Drain to Source Voltage >= 30V`). |
| `search_catalog` | Search the downloaded JLCPCB/LCSC parts catalogue by words and/or a category with parameter filters (`Drain to Source Voltage >= 30V`, `RDS(on) <= 10mΩ`, `Resistance = 10k`, `Type = N-Channel`, `Package = SOT-23|SOT-23-3`), sorted by stock, price or a parameter, with paging. Shows stock, JLC basic/extended, price, package, the filtered parameters and whether the library has the part. The user downloads the catalogue in Settings. |
| `import_datasheets` | Import many datasheets at once, in parallel: part numbers, LCSC codes from `search_catalog`, URLs, paths or a folder (optionally recursive). One line per import with its outcome. |
| `import_bom` | Build a project from a BOM (CSV/TSV text or file from KiCad, Altium, JLCPCB…): parts found in the library or fetched by part number; unfetchable parts join as "no datasheet"; lines without a part number are skipped. |
| `update_part` | Correct part number, manufacturer, description, category, package, tags; add orderable variants or more part numbers on the same datasheet; set family patterns (`STM32U545xx`); star it (`favorite`) or set its `status` (preferred, approved, avoid, nrnd, eol). |
| `record_parameters` | Add or update parameters with key, section and page; `replace_auto` drops automatic rows for the same key; `delete` removes wrong rows. |
| `describe_figure` | Record what a figure shows: kind, title, axes with ranges, conditions, description, curves. |
| `record_table` | Save a clean transcription of a table read from its image (header with units, one row per entry; matrices flattened to one row per combination). Replaces the automatic cells, makes them searchable, survives re-extraction. Can also add a table the extractor missed (page + region). |
| `record_pins` | Replace the pin table (or one package's pins). |
| `record_registers` | Add registers with address, width, reset, access and bit fields. |
| `record_recommendations` | Recommended components: role, value, part number, manufacturer, conditions, page. Regular "recommended inductors/capacitors" tables are turned into recommendations automatically. |
| `add_note` | A markdown note on a part, figure, parameter, datasheet or project. |
| `list_notes` | Notes on a target, or all notes. |

## Projects

| Tool | What it does |
|---|---|
| `list_projects` | Projects with part and rail counts. |
| `create_project` | Create a project (optionally the active one, which project arguments default to). |
| `get_project` | BOM, rails with load (direct loads plus regulator input currents derived from their output loads, chargers drawing the charge current set as their current (none set: no charging; a linear charger draws the larger of that and its load, a power-path charger both, up to its input limit); a part with the same supply and output rail counts as a load on it, and loops in the power tree are reported), each rail's ripple budget (peak-to-peak ripple it can carry before a part on it leaves its recommended input range, and the limiting part), capacity and power, input power, checks of every rail against each part's absolute-maximum and recommended limits, saved calculations, notes. |
| `set_rail` | Add, update or delete a rail: nominal/min/max voltage, current limit, source, and loads on it that aren't in the BOM. |
| `add_to_project` | Place a part with designator, quantity, role, supply rail, output rail and load current. |
| `update_project_part` | Change or remove a BOM entry by id or designator; `clear_currents` unsets its currents (a regulator's input is then derived, a charger counts no charging). |
| `set_connections` | Draft or edit the project's block diagram and connection table: `blocks` off the BOM with a `kind` (connector, module, resistor, capacitor, inductor, diode) and a `value` (a connector with its pins, a battery, passives not in the library: R4 36.5k, C1 10µF), `connect` pins to nets (BOM designator, part number or block; pin number or name, or `U1.SDA`; a passive's two ends as pins 1 and 2, or one pinless connection to hang it from a net to ground), `nets` to set a net's kind (signal, power, ground), bus or I/O voltage, and `positions` to place parts on the diagram (`reset_layout` goes back to the automatic layout). A pin is on one net; connecting it again moves it. A net named like a rail is that rail, GND and VSS are ground. Pins are checked against the part's pin table, and what couldn't be applied is returned with the checks. |
| `get_connections` | The connection table (each net with its pins, their names and types from the pin tables), each part's pins with what they're connected to (every pin of its pin table, connected or not), the block diagram's links (power from the part that drives a net to the parts it feeds, signals per bus or net; from the connection table only) and the checks: pins missing from a pin table, ground or supply pins on the wrong net, pins that must be connected left open on parts being connected (supply, ground, exposed pads, and pins whose datasheet says they mustn't float), nets with one connection, two outputs on a net. |

## Calculators

| Tool | What it does |
|---|---|
| `list_calculators` | Calculators, their inputs, and the canonical parameter keys. |
| `calculate` | Run `buck`, `boost`, `ldo`, `gate_drive`, `gate_divider` or `divider`. With `part`, inputs are filled from the datasheet (switching frequency, reference voltage, current limit, minimum on/off time, gate charge, …); with `project`, from the part's rails. The result lists every filled value with its page, outputs with formulas, checks against device limits, and suggestions. `save` stores it in the project. |

## Canonical parameter keys

Calculators and project checks find values by key. `list_calculators`
prints the full list; the common ones are `vin`, `vout`, `iout`, `fsw`,
`vref`, `ilim` (steady-state peak/valley limit; `ilim_startup` and `ilim_in`
are the start-up and average input limits), `ton_min`, `toff_min`, `iq`, `uvlo`, `rdson`, `rdson_hs`,
`rdson_ls`, `vds`, `vgs`, `vgs_th`, `qg`, `qgd`, `ciss`, `vdropout`,
`rth_ja` and `tj`. A key's section matters: `vin` under `abs_max` is the
rating, under `recommended` the operating range.

## Example session

```text
> Import the TPS563200 datasheet and design a 12 V → 3.3 V, 2 A supply with it.

import_datasheet {part_number: "TPS563200"}
get_part {part: "TPS563200"}                       key parameters, sections
get_figure {part: "TPS563200", figure: "8-16"}     the reference schematic, as an image
record_recommendations {part: "TPS563200", …}      inductor and capacitor values read from it
calculate {kind: "buck", part: "TPS563200",
           inputs: {vinMin: 10.8, vinMax: 13.2, vout: 3.3, iout: 2}, show: true}
```
